// Package osrm is a real-road router backed by an OSRM server
// (https://project-osrm.org/).
//
// Every leg is a pair of points. Each point is rounded to 1e-5 degrees
// (about 1 m) before it is sent to OSRM or used as a cache key, so the same
// inputs always produce the same request. A pair's duration and distance
// always come from OSRM's /table service (annotations=duration,distance),
// whether it was fetched one pair at a time or as part of a many-to-many
// prefetch, so results never depend on call order. The polyline comes from
// /route (overview=full, geometries=polyline, which is Google's encoded
// polyline format at precision 5) and is fetched only when Route needs it.
// Durations and distances are rounded to whole seconds and meters, halves
// away from zero, like the straight-line router.
package osrm

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/polyline"
)

// Defaults for the [osrm] settings.
const (
	DefaultURL     = "http://127.0.0.1:5000"
	DefaultProfile = "car"
	// maxTableSources caps the sources in one /table request. osrm_setup.sh
	// starts osrm-routed with a larger --max-table-size than this.
	maxTableSources = 100
	// parallelRequests caps concurrent /table requests.
	parallelRequests = 8
)

// Options configure the router.
type Options struct {
	URL       string
	Profile   string
	CachePath string // optional JSON-lines cache file, read at start and appended to
	Dataset   Dataset
	Client    *http.Client
}

// Dataset names the map extract OSRM was built from.
type Dataset struct {
	Name   string `json:"name"`
	Date   string `json:"download_date"`
	SHA256 string `json:"sha256"`
	// OSRMVersion is the osrm-backend version that prepared the map.
	OSRMVersion string `json:"osrm_version"`
}

// point is a location rounded to 1e-5 degrees, as integers.
type point struct{ lat, lng int64 }

type pair struct{ from, to point }

type cost struct {
	Duration int64 `json:"d"`
	Distance int64 `json:"m"`
}

// Router is the OSRM router.
type Router struct {
	opts       Options
	version    string
	offline    bool
	mu         sync.Mutex
	costs      map[pair]cost // pairs a leg has used, kept for the whole run
	prefetched map[pair]cost // pairs from the latest Prefetch call
	persisted  map[pair]bool
	geometries map[pair][]model.Location
	cacheFile  *os.File
	cacheOut   *bufio.Writer
	// tableRequests and routeRequests count HTTP requests sent to OSRM.
	tableRequests, routeRequests atomic.Int64
	// cacheHits counts pairs served from memory or the cache file without a
	// new HTTP request (costs already known, or a geometry already loaded).
	cacheHits atomic.Int64
	// cacheMisses counts pairs that needed a new HTTP request.
	cacheMisses atomic.Int64
}

func round5(v float64) int64 { return int64(math.Round(v * 1e5)) }

func toPoint(l model.Location) point { return point{round5(l.Lat), round5(l.Lng)} }

func (p point) location() model.Location {
	return model.Location{Lat: float64(p.lat) / 1e5, Lng: float64(p.lng) / 1e5}
}

// coordinate formats a point the way OSRM expects it: lng,lat.
func (p point) coordinate() string {
	return formatE5(p.lng) + "," + formatE5(p.lat)
}

func formatE5(v int64) string {
	return strconv.FormatFloat(float64(v)/1e5, 'f', 5, 64)
}

// New checks that OSRM answers (failing fast with a clear error), loads the
// cache file if any and returns the router. If OSRM is unreachable but the
// cache file has entries, the router runs from the cache alone and fails on
// any pair it doesn't hold.
func New(opts Options) (*Router, error) {
	if opts.URL == "" {
		opts.URL = DefaultURL
	}
	opts.URL = strings.TrimRight(opts.URL, "/")
	if opts.Profile == "" {
		opts.Profile = DefaultProfile
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 60 * time.Second}
	}
	r := &Router{opts: opts, costs: map[pair]cost{}, persisted: map[pair]bool{}, geometries: map[pair][]model.Location{}}
	if opts.CachePath != "" {
		if err := r.loadCache(); err != nil {
			return nil, err
		}
	}
	if err := r.healthCheck(); err != nil {
		if len(r.costs) == 0 {
			return nil, fmt.Errorf("osrm: no OSRM server at %s (%v); start one with scripts/osrm_setup.sh or set [osrm] url", opts.URL, err)
		}
		fmt.Fprintf(os.Stderr, "rhs: osrm: server at %s unreachable (%v); routing from cache %s only\n", opts.URL, err, opts.CachePath)
		r.offline = true
	}
	if opts.CachePath != "" {
		f, err := os.OpenFile(opts.CachePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("osrm: cache: %w", err)
		}
		r.cacheFile, r.cacheOut = f, bufio.NewWriter(f)
	}
	return r, nil
}

// RouterMetadata implements model.RouterMetadataProvider.
func (r *Router) RouterMetadata() *model.RouterMetadata {
	version := r.version
	if version == "" {
		version = r.opts.Dataset.OSRMVersion
	}
	return &model.RouterMetadata{Name: "osrm", Profile: r.opts.Profile, EngineVersion: version,
		DatasetName: r.opts.Dataset.Name, DatasetDate: r.opts.Dataset.Date, DatasetSHA256: r.opts.Dataset.SHA256}
}

// Close flushes and closes the cache file.
func (r *Router) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cacheFile == nil {
		return nil
	}
	err := r.cacheOut.Flush()
	if cerr := r.cacheFile.Close(); err == nil {
		err = cerr
	}
	r.cacheFile = nil
	return err
}

// healthCheck asks /nearest for one point and reads the engine version
// from the Server or X-Powered-By headers or a /version endpoint, when the
// server exposes one.
func (r *Router) healthCheck() error {
	resp, err := r.opts.Client.Get(r.opts.URL + "/nearest/v1/" + r.opts.Profile + "/-97.74306,30.26715?number=1")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("unexpected response %q", truncate(string(body)))
	}
	// NoSegment just means the probe point is outside the map; the server works.
	if parsed.Code != "Ok" && parsed.Code != "NoSegment" {
		return fmt.Errorf("%s: %s", parsed.Code, parsed.Message)
	}
	for _, h := range []string{"X-Osrm-Version", "Server"} {
		if v := resp.Header.Get(h); strings.Contains(strings.ToLower(v), "osrm") || h == "X-Osrm-Version" && v != "" {
			r.version = v
			break
		}
	}
	return nil
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// Route implements model.Router.
func (r *Router) Route(start model.Location, waypoints []model.Location, departAt int64) ([]model.Leg, error) {
	return r.legs(start, waypoints, true)
}

// Estimate implements model.LegEstimator: the same durations and distances
// as Route, without polylines.
func (r *Router) Estimate(start model.Location, waypoints []model.Location, departAt int64) ([]model.Leg, error) {
	return r.legs(start, waypoints, false)
}

func (r *Router) legs(start model.Location, waypoints []model.Location, withGeometry bool) ([]model.Leg, error) {
	pairs := make([]pair, len(waypoints))
	from := toPoint(start)
	for i, w := range waypoints {
		to := toPoint(w)
		pairs[i] = pair{from, to}
		from = to
	}
	r.mu.Lock()
	for _, p := range pairs {
		if c, ok := r.prefetched[p]; ok {
			if _, have := r.costs[p]; !have {
				r.costs[p] = c
			}
		}
	}
	r.mu.Unlock()
	if err := r.ensureCosts(pairs, r.costs); err != nil {
		return nil, err
	}
	legs := make([]model.Leg, len(waypoints))
	from0 := start
	for i, p := range pairs {
		r.mu.Lock()
		c := r.costs[p]
		// Only legs actually used reach the cache file; most prefetched
		// pairs never are, and a replay asks only for these.
		if p.from != p.to && !r.persisted[p] {
			r.persisted[p] = true
			r.appendCache(cacheLine{Kind: "c", From: [2]int64{p.from.lat, p.from.lng}, To: [2]int64{p.to.lat, p.to.lng}, Cost: &c})
		}
		r.mu.Unlock()
		segment := model.Segment{DistanceMeters: c.Distance, DurationSeconds: c.Duration}
		if withGeometry {
			shape, err := r.geometry(p)
			if err != nil {
				return nil, err
			}
			// Start and end the line exactly on the stops, so a car never
			// jumps between a stop and OSRM's snapped road position.
			segment.Polyline = append(append([]model.Location{from0}, shape...), waypoints[i])
		}
		legs[i] = model.Leg{Segments: []model.Segment{segment}, DistanceMeters: c.Distance, DurationSeconds: c.Duration}
		from0 = waypoints[i]
	}
	return legs, nil
}

// Prefetch implements model.LegPrefetcher with /table requests for the
// pairs not already cached. Prefetched pairs are kept only until the next
// Prefetch call unless a leg uses them: cars move, so most pairs are never
// asked for again, and keeping them all would grow without bound. Dropping
// one is safe because refetching it returns the same value.
func (r *Router) Prefetch(sources, destinations []model.Location, departAt int64) error {
	var pairs []pair
	for _, s := range sources {
		for _, d := range destinations {
			pairs = append(pairs, pair{toPoint(s), toPoint(d)})
		}
	}
	r.mu.Lock()
	next := map[pair]cost{}
	for _, p := range pairs {
		if c, ok := r.prefetched[p]; ok {
			next[p] = c
		}
	}
	r.prefetched = next
	r.mu.Unlock()
	return r.ensureCosts(pairs, next)
}

// ensureCosts fetches every pair in neither r.costs nor target into
// target, grouping them into /table requests.
func (r *Router) ensureCosts(pairs []pair, target map[pair]cost) error {
	r.mu.Lock()
	missing := map[pair]bool{}
	hits := int64(0)
	for _, p := range pairs {
		_, known := r.costs[p]
		_, fetched := target[p]
		if known || fetched {
			hits++
			continue
		}
		if p.from == p.to {
			target[p] = cost{}
			continue
		}
		missing[p] = true
	}
	r.mu.Unlock()
	if hits > 0 {
		r.cacheHits.Add(hits)
	}
	r.cacheMisses.Add(int64(len(missing)))
	if len(missing) == 0 {
		return nil
	}
	if r.offline {
		for p := range missing {
			return fmt.Errorf("osrm: pair %s -> %s is not in cache %s and the server is unreachable", p.from.coordinate(), p.to.coordinate(), r.opts.CachePath)
		}
	}
	sourceSet, destSet := map[point]bool{}, map[point]bool{}
	for p := range missing {
		sourceSet[p.from], destSet[p.to] = true, true
	}
	sources, dests := sortedPoints(sourceSet), sortedPoints(destSet)
	type chunk struct{ sources, dests []point }
	var chunks []chunk
	for i := 0; i < len(sources); i += maxTableSources {
		for j := 0; j < len(dests); j += maxTableSources {
			chunks = append(chunks, chunk{sources[i:min(i+maxTableSources, len(sources))], dests[j:min(j+maxTableSources, len(dests))]})
		}
	}
	// Chunks run in parallel; results are per pair, so order doesn't matter.
	errs := make([]error, len(chunks))
	var wg sync.WaitGroup
	limit := make(chan struct{}, parallelRequests)
	for k, c := range chunks {
		wg.Add(1)
		limit <- struct{}{}
		go func(k int, c chunk) {
			defer wg.Done()
			defer func() { <-limit }()
			errs[k] = r.table(c.sources, c.dests, target)
		}(k, c)
	}
	wg.Wait()
	return errors.Join(errs...)
}

func sortedPoints(set map[point]bool) []point {
	out := make([]point, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].lat != out[j].lat {
			return out[i].lat < out[j].lat
		}
		return out[i].lng < out[j].lng
	})
	return out
}

// RequestCounts returns how many /table and /route requests the router has
// sent to the OSRM server so far. Cache hits send none.
func (r *Router) RequestCounts() (table, route int64) {
	return r.tableRequests.Load(), r.routeRequests.Load()
}

// CacheMisses returns how many pairs needed a new HTTP request.
func (r *Router) CacheMisses() int64 { return r.cacheMisses.Load() }

// CacheHits returns how many pairs were served from memory or the cache
// file without a new HTTP request.
func (r *Router) CacheHits() int64 {
	return r.cacheHits.Load()
}

func (r *Router) get(path string, out any) error {
	if strings.HasPrefix(path, "/table/") {
		r.tableRequests.Add(1)
	} else {
		r.routeRequests.Add(1)
	}
	resp, err := r.opts.Client.Get(r.opts.URL + path)
	if err != nil {
		return fmt.Errorf("osrm: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("osrm: %w", err)
	}
	var status struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("osrm: HTTP %d: %s", resp.StatusCode, truncate(string(body)))
	}
	if status.Code != "Ok" {
		return fmt.Errorf("osrm: %s: %s", status.Code, status.Message)
	}
	return json.Unmarshal(body, out)
}

// table fetches durations and distances from every source to every
// destination in one request. The same points serve as both lists, with
// sources and destinations given as indexes.
func (r *Router) table(sources, dests []point, target map[pair]cost) error {
	coords := make([]string, 0, len(sources)+len(dests))
	var srcIdx, dstIdx []string
	for i, p := range sources {
		coords = append(coords, p.coordinate())
		srcIdx = append(srcIdx, strconv.Itoa(i))
	}
	for i, p := range dests {
		coords = append(coords, p.coordinate())
		dstIdx = append(dstIdx, strconv.Itoa(len(sources)+i))
	}
	path := "/table/v1/" + r.opts.Profile + "/" + strings.Join(coords, ";") + "?annotations=duration,distance" +
		"&sources=" + strings.Join(srcIdx, ";") + "&destinations=" + strings.Join(dstIdx, ";")
	var parsed struct {
		Durations [][]*float64 `json:"durations"`
		Distances [][]*float64 `json:"distances"`
	}
	if err := r.get(path, &parsed); err != nil {
		return err
	}
	if len(parsed.Durations) != len(sources) || len(parsed.Distances) != len(sources) {
		return errors.New("osrm: /table returned the wrong number of rows")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range sources {
		if len(parsed.Durations[i]) != len(dests) || len(parsed.Distances[i]) != len(dests) {
			return errors.New("osrm: /table returned the wrong number of columns")
		}
		for j, d := range dests {
			if s == d {
				target[pair{s, d}] = cost{}
				continue
			}
			du, di := parsed.Durations[i][j], parsed.Distances[i][j]
			if du == nil || di == nil {
				return fmt.Errorf("osrm: no route from %s to %s", s.coordinate(), d.coordinate())
			}
			c := cost{Duration: int64(math.Round(*du)), Distance: int64(math.Round(*di))}
			target[pair{s, d}] = c
		}
	}
	return nil
}

// geometry returns the road shape between a pair, from /route.
func (r *Router) geometry(p pair) ([]model.Location, error) {
	if p.from == p.to {
		return nil, nil
	}
	r.mu.Lock()
	shape, ok := r.geometries[p]
	r.mu.Unlock()
	if ok {
		r.cacheHits.Add(1)
		return shape, nil
	}
	r.cacheMisses.Add(1)
	if r.offline {
		return nil, fmt.Errorf("osrm: route %s -> %s is not in cache %s and the server is unreachable", p.from.coordinate(), p.to.coordinate(), r.opts.CachePath)
	}
	var parsed struct {
		Routes []struct {
			Geometry string `json:"geometry"`
		} `json:"routes"`
	}
	path := "/route/v1/" + r.opts.Profile + "/" + p.from.coordinate() + ";" + p.to.coordinate() +
		"?overview=full&geometries=polyline&steps=false&alternatives=false"
	if err := r.get(path, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Routes) == 0 {
		return nil, fmt.Errorf("osrm: no route from %s to %s", p.from.coordinate(), p.to.coordinate())
	}
	shape = polyline.Decode(parsed.Routes[0].Geometry)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.geometries[p] = shape
	r.appendCache(cacheLine{Kind: "g", From: [2]int64{p.from.lat, p.from.lng}, To: [2]int64{p.to.lat, p.to.lng}, Polyline: parsed.Routes[0].Geometry})
	return shape, nil
}

// cacheLine is one JSON line of the cache file: a pair's cost ("c") or its
// encoded polyline ("g"). The first line is a header naming the dataset.
type cacheLine struct {
	Kind     string   `json:"k"`
	From     [2]int64 `json:"f,omitempty"`
	To       [2]int64 `json:"t,omitempty"`
	Cost     *cost    `json:"c,omitempty"`
	Polyline string   `json:"p,omitempty"`
	Profile  string   `json:"profile,omitempty"`
	Dataset  string   `json:"dataset_sha256,omitempty"`
}

func (r *Router) appendCache(line cacheLine) {
	if r.cacheOut == nil {
		return
	}
	encoded, _ := json.Marshal(line)
	r.cacheOut.Write(append(encoded, '\n'))
}

func (r *Router) loadCache() error {
	f, err := os.Open(r.opts.CachePath)
	if errors.Is(err, os.ErrNotExist) {
		return r.writeHeader()
	}
	if err != nil {
		return fmt.Errorf("osrm: cache: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	first := true
	for scanner.Scan() {
		var line cacheLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue // a torn last line from an interrupted run
		}
		if first {
			first = false
			if line.Kind != "h" {
				return fmt.Errorf("osrm: cache %s has no header line", r.opts.CachePath)
			}
			if line.Profile != r.opts.Profile || line.Dataset != r.opts.Dataset.SHA256 {
				return fmt.Errorf("osrm: cache %s was built for profile %q and dataset %q, not %q and %q; delete it or set another cache_path",
					r.opts.CachePath, line.Profile, line.Dataset, r.opts.Profile, r.opts.Dataset.SHA256)
			}
			continue
		}
		p := pair{point{line.From[0], line.From[1]}, point{line.To[0], line.To[1]}}
		switch {
		case line.Kind == "c" && line.Cost != nil:
			r.costs[p] = *line.Cost
			r.persisted[p] = true
		case line.Kind == "g":
			r.geometries[p] = polyline.Decode(line.Polyline)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("osrm: cache: %w", err)
	}
	if first {
		return r.writeHeader()
	}
	return nil
}

func (r *Router) writeHeader() error {
	encoded, _ := json.Marshal(cacheLine{Kind: "h", Profile: r.opts.Profile, Dataset: r.opts.Dataset.SHA256})
	return os.WriteFile(r.opts.CachePath, append(encoded, '\n'), 0o644)
}

// ReadDatasetMetadata reads the JSON file scripts/osrm_setup.sh writes next
// to the prepared map.
func ReadDatasetMetadata(path string) (Dataset, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Dataset{}, fmt.Errorf("osrm: dataset metadata: %w", err)
	}
	var meta struct {
		Region    string `json:"region"`
		SourceURL string `json:"source_url"`
		Dataset
	}
	if err := json.Unmarshal(content, &meta); err != nil {
		return Dataset{}, fmt.Errorf("osrm: dataset metadata %s: %w", path, err)
	}
	if meta.Name == "" {
		meta.Name = meta.Region
		if u, err := url.Parse(meta.SourceURL); err == nil && meta.SourceURL != "" {
			meta.Name += " (" + u.Path[strings.LastIndex(u.Path, "/")+1:] + ")"
		}
	}
	return meta.Dataset, nil
}

// Round returns a location rounded the way the router keys and sends it.
func Round(l model.Location) model.Location { return toPoint(l).location() }
