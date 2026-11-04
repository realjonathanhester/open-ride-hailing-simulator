package osrm

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/polyline"
)

// Points in Austin.
var (
	capitol    = model.Location{Lat: 30.27467, Lng: -97.74035}
	zilker     = model.Location{Lat: 30.26698, Lng: -97.77289}
	university = model.Location{Lat: 30.28490, Lng: -97.73410}
)

// fakeOSRM answers /nearest, /table and /route like osrm-routed. A pair's
// duration is half its Manhattan distance in 1e-5 degrees, floored, plus 0.5,
// so every value is a half and rounding is exercised; its distance is ten
// times that. The route shape
// is the midpoint of the pair.
type fakeOSRM struct {
	mu       sync.Mutex
	requests map[string]int
	nullPair bool
}

func coordinates(path string) []model.Location {
	parts := strings.Split(path, "/")
	var out []model.Location
	for _, c := range strings.Split(parts[len(parts)-1], ";") {
		ll := strings.Split(c, ",")
		lng, _ := strconv.ParseFloat(ll[0], 64)
		lat, _ := strconv.ParseFloat(ll[1], 64)
		out = append(out, model.Location{Lat: lat, Lng: lng})
	}
	return out
}

func fakeDuration(a, b model.Location) float64 {
	return math.Floor(math.Round((math.Abs(a.Lat-b.Lat)+math.Abs(a.Lng-b.Lng))*1e5)/2) + 0.5
}

// query reads one parameter by hand: OSRM separates list items with ";",
// which net/url refuses to parse.
func query(r *http.Request, name string) string {
	for _, kv := range strings.Split(r.URL.RawQuery, "&") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return ""
}

func indexes(list string) []int {
	var out []int
	for _, s := range strings.Split(list, ";") {
		i, _ := strconv.Atoi(s)
		out = append(out, i)
	}
	return out
}

func (f *fakeOSRM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	service := strings.Split(r.URL.Path, "/")[1]
	f.mu.Lock()
	f.requests[service]++
	f.mu.Unlock()
	points := coordinates(r.URL.Path)
	var body any
	switch service {
	case "nearest":
		body = map[string]any{"code": "Ok", "waypoints": []any{}}
	case "table":
		var durations, distances [][]*float64
		for _, i := range indexes(query(r, "sources")) {
			var durationRow, distanceRow []*float64
			for _, j := range indexes(query(r, "destinations")) {
				duration, distance := fakeDuration(points[i], points[j]), fakeDuration(points[i], points[j])*10
				if f.nullPair {
					durationRow, distanceRow = append(durationRow, nil), append(distanceRow, nil)
					continue
				}
				durationRow, distanceRow = append(durationRow, &duration), append(distanceRow, &distance)
			}
			durations, distances = append(durations, durationRow), append(distances, distanceRow)
		}
		body = map[string]any{"code": "Ok", "durations": durations, "distances": distances}
	case "route":
		mid := model.Location{Lat: (points[0].Lat + points[1].Lat) / 2, Lng: (points[0].Lng + points[1].Lng) / 2}
		body = map[string]any{"code": "Ok", "routes": []any{map[string]any{"geometry": polyline.Encode([]model.Location{mid})}}}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

func (f *fakeOSRM) count(service string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[service]
}

func startFake(t *testing.T) (*fakeOSRM, *httptest.Server) {
	t.Helper()
	fake := &fakeOSRM{requests: map[string]int{}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func newRouter(t *testing.T, opts Options) *Router {
	t.Helper()
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func assertEquals[V comparable](t *testing.T, got, want V) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRouteRoundsDurationAndDistanceLikeTheStraightLineRouter(t *testing.T) {
	_, server := startFake(t)
	r := newRouter(t, Options{URL: server.URL})

	legs, err := r.Route(capitol, []model.Location{zilker}, 0)

	assertEquals(t, err, nil)
	assertEquals(t, len(legs), 1)
	assertEquals(t, legs[0].DurationSeconds, int64(2012)) // OSRM says 2011.5
	assertEquals(t, legs[0].DistanceMeters, int64(20115)) // OSRM says 20115.0
	assertEquals(t, legs[0].Segments[0].DurationSeconds, int64(2012))
	assertEquals(t, legs[0].Segments[0].DistanceMeters, int64(20115))
}

func TestRoutePolylineStartsAndEndsOnTheStops(t *testing.T) {
	_, server := startFake(t)
	r := newRouter(t, Options{URL: server.URL})

	legs, _ := r.Route(capitol, []model.Location{zilker}, 0)

	shape := legs[0].Segments[0].Polyline
	assertEquals(t, len(shape), 3)
	assertEquals(t, shape[0], capitol)
	assertEquals(t, shape[1], model.Location{Lat: 30.27083, Lng: -97.75662})
	assertEquals(t, shape[2], zilker)
}

func TestRouteReturnsOneLegPerWaypoint(t *testing.T) {
	_, server := startFake(t)
	r := newRouter(t, Options{URL: server.URL})

	legs, _ := r.Route(capitol, []model.Location{zilker, university}, 0)

	assertEquals(t, len(legs), 2)
	assertEquals(t, legs[1].DurationSeconds, int64(int64(math.Round(fakeDuration(zilker, university)))))
	assertEquals(t, legs[1].Segments[0].Polyline[0], zilker)
}

func TestZeroLengthLegNeedsNoRequest(t *testing.T) {
	fake, server := startFake(t)
	r := newRouter(t, Options{URL: server.URL})

	legs, _ := r.Route(capitol, []model.Location{capitol}, 0)

	assertEquals(t, legs[0].DurationSeconds, int64(0))
	assertEquals(t, legs[0].DistanceMeters, int64(0))
	assertEquals(t, fake.count("table"), 0)
	assertEquals(t, fake.count("route"), 0)
}

func TestPrefetchFetchesManyToManyInOneTableRequest(t *testing.T) {
	fake, server := startFake(t)
	r := newRouter(t, Options{URL: server.URL})

	err := r.Prefetch([]model.Location{capitol, zilker, university}, []model.Location{zilker, university}, 0)

	assertEquals(t, err, nil)
	assertEquals(t, fake.count("table"), 1)
}

func TestEstimateAfterPrefetchMakesNoRequests(t *testing.T) {
	fake, server := startFake(t)
	r := newRouter(t, Options{URL: server.URL})
	r.Prefetch([]model.Location{capitol, zilker}, []model.Location{university}, 0)

	legs, _ := r.Estimate(capitol, []model.Location{university}, 0)

	assertEquals(t, legs[0].DurationSeconds, int64(math.Round(fakeDuration(capitol, university))))
	assertEquals(t, len(legs[0].Segments[0].Polyline), 0)
	assertEquals(t, fake.count("table"), 1)
	assertEquals(t, fake.count("route"), 0)
}

func TestRouteAndEstimateAgreeWhetherOrNotPrefetched(t *testing.T) {
	_, server := startFake(t)
	prefetched := newRouter(t, Options{URL: server.URL})
	prefetched.Prefetch([]model.Location{capitol, zilker, university}, []model.Location{capitol, zilker, university}, 0)
	cold := newRouter(t, Options{URL: server.URL})

	a, _ := prefetched.Estimate(capitol, []model.Location{zilker, university}, 0)
	b, _ := cold.Route(capitol, []model.Location{zilker, university}, 0)

	assertEquals(t, a[0].DurationSeconds, b[0].DurationSeconds)
	assertEquals(t, a[1].DurationSeconds, b[1].DurationSeconds)
	assertEquals(t, a[1].DistanceMeters, b[1].DistanceMeters)
}

func TestCoordinatesAreRoundedToOneMeterForCaching(t *testing.T) {
	fake, server := startFake(t)
	r := newRouter(t, Options{URL: server.URL})
	r.Route(capitol, []model.Location{zilker}, 0)

	r.Route(model.Location{Lat: capitol.Lat + 2e-6, Lng: capitol.Lng - 3e-6}, []model.Location{zilker}, 0)

	assertEquals(t, fake.count("table"), 1)
	assertEquals(t, fake.count("route"), 1)
}

func TestCacheFileReplaysARunWithoutTheServer(t *testing.T) {
	_, server := startFake(t)
	cache := filepath.Join(t.TempDir(), "cache.jsonl")
	first := newRouter(t, Options{URL: server.URL, CachePath: cache})
	want, _ := first.Route(capitol, []model.Location{zilker, university}, 0)
	first.Close()
	server.Close()

	second := newRouter(t, Options{URL: server.URL, CachePath: cache})
	got, err := second.Route(capitol, []model.Location{zilker, university}, 0)

	assertEquals(t, err, nil)
	assertEquals(t, got[0].DurationSeconds, want[0].DurationSeconds)
	assertEquals(t, got[1].DistanceMeters, want[1].DistanceMeters)
	assertEquals(t, polyline.Encode(got[1].Segments[0].Polyline), polyline.Encode(want[1].Segments[0].Polyline))
}

func TestCacheMissWithoutTheServerIsAnError(t *testing.T) {
	_, server := startFake(t)
	cache := filepath.Join(t.TempDir(), "cache.jsonl")
	first := newRouter(t, Options{URL: server.URL, CachePath: cache})
	first.Route(capitol, []model.Location{zilker}, 0)
	first.Close()
	server.Close()

	second := newRouter(t, Options{URL: server.URL, CachePath: cache})
	_, err := second.Route(capitol, []model.Location{university}, 0)

	assertEquals(t, err != nil && strings.Contains(err.Error(), "not in cache"), true)
}

func TestCacheForAnotherDatasetIsRejected(t *testing.T) {
	_, server := startFake(t)
	cache := filepath.Join(t.TempDir(), "cache.jsonl")
	newRouter(t, Options{URL: server.URL, CachePath: cache, Dataset: Dataset{SHA256: "aaa"}}).Close()

	_, err := New(Options{URL: server.URL, CachePath: cache, Dataset: Dataset{SHA256: "bbb"}})

	assertEquals(t, err != nil && strings.Contains(err.Error(), "was built for"), true)
}

func TestUnreachableServerFailsFast(t *testing.T) {
	_, server := startFake(t)
	server.Close()

	_, err := New(Options{URL: server.URL})

	assertEquals(t, err != nil && strings.Contains(err.Error(), "no OSRM server at "+server.URL), true)
}

func TestUnroutablePairIsAnError(t *testing.T) {
	fake, server := startFake(t)
	fake.nullPair = true
	r := newRouter(t, Options{URL: server.URL})

	_, err := r.Route(capitol, []model.Location{zilker}, 0)

	assertEquals(t, err != nil && strings.Contains(err.Error(), "no route from"), true)
}

func TestRouterMetadataNamesTheDataset(t *testing.T) {
	_, server := startFake(t)
	r := newRouter(t, Options{URL: server.URL, Dataset: Dataset{Name: "austin", Date: "2026-11-04", SHA256: "abc", OSRMVersion: "v26.11.0"}})

	meta := r.RouterMetadata()

	assertEquals(t, meta.Name, "osrm")
	assertEquals(t, meta.Profile, "car")
	assertEquals(t, meta.EngineVersion, "v26.11.0")
	assertEquals(t, meta.DatasetName, "austin")
	assertEquals(t, meta.DatasetDate, "2026-11-04")
	assertEquals(t, meta.DatasetSHA256, "abc")
}

func TestReadDatasetMetadataReadsTheSetupScriptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.json")
	os.WriteFile(path, []byte(`{"region":"austin","name":"austin (texas-latest.osm.pbf, cropped)","download_date":"2026-11-04","sha256":"abc","osrm_version":"v26.11.0"}`), 0o644)

	d, err := ReadDatasetMetadata(path)

	assertEquals(t, err, nil)
	assertEquals(t, d.Name, "austin (texas-latest.osm.pbf, cropped)")
	assertEquals(t, d.Date, "2026-11-04")
	assertEquals(t, d.SHA256, "abc")
	assertEquals(t, d.OSRMVersion, "v26.11.0")
}

// TestAgainstRealOSRM runs only when OSRM_URL points at a server prepared by
// scripts/osrm_setup.sh --region austin.
func TestAgainstRealOSRM(t *testing.T) {
	url := os.Getenv("OSRM_URL")
	if url == "" {
		t.Skip("OSRM_URL not set")
	}
	r := newRouter(t, Options{URL: url})
	r.Prefetch([]model.Location{capitol, zilker}, []model.Location{university}, 0)

	legs, err := r.Route(capitol, []model.Location{zilker, university}, 0)

	assertEquals(t, err, nil)
	assertEquals(t, legs[0].DurationSeconds > 60, true)
	assertEquals(t, legs[0].DistanceMeters > 3000, true)
	assertEquals(t, len(legs[1].Segments[0].Polyline) > 2, true)
}
