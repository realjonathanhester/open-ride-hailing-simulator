package viewer

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed ui
var uiFiles embed.FS

// DefaultPageSize is the number of rows per page for long tables.
const DefaultPageSize = 100

// Config configures the viewer server.
type Config struct {
	Run              *Run
	Compare          *Run // optional second run
	GoogleMapsAPIKey string
	GoogleMapsMapID  string
	// Timezone is the resolved IANA zone (see ResolveTimezone); empty means
	// the browser's local zone. TimezoneSource says where it came from.
	Timezone       string
	TimezoneSource string
}

// Server serves the UI and the JSON API.
type Server struct {
	config  Config
	mux     *http.ServeMux
	handler http.Handler

	compareOnce sync.Once
	comparison  Comparison
}

// NewServer builds the HTTP handler.
func NewServer(config Config) *Server {
	s := &Server{config: config, mux: http.NewServeMux()}
	static, _ := fs.Sub(uiFiles, "ui")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/dashboard", s.handleDashboard)
	s.mux.HandleFunc("GET /api/trips", s.handleTrips)
	s.mux.HandleFunc("GET /api/trips/{request_id}", s.handleTrip)
	s.mux.HandleFunc("GET /api/supply", s.handleSupplyList)
	s.mux.HandleFunc("GET /api/supply/{supply_id}", s.handleSupply)
	s.mux.HandleFunc("GET /api/compare", s.handleCompare)
	s.mux.HandleFunc("GET /api/compare/trips", s.handleCompareTrips)
	s.mux.HandleFunc("GET /api/compare/trips/{request_id}", s.handleCompareTrip)
	s.mux.HandleFunc("GET /api/decisions", s.handleDecisions)
	s.mux.HandleFunc("GET /api/playback", s.handlePlayback)
	s.mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "unknown endpoint") })
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.handler = gzipResponses(s.mux)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// Warm computes the cached dashboards, request indexes and comparison so the
// first page load does not pay for them. rhs view runs it in the background.
func (s *Server) Warm() {
	s.config.Run.Warm()
	if s.config.Compare != nil {
		s.config.Compare.Warm()
		s.cachedComparison()
	}
}

func (s *Server) cachedComparison() Comparison {
	s.compareOnce.Do(func() { s.comparison = Compare(s.config.Run, s.config.Compare) })
	return s.comparison
}

type gzipWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (g gzipWriter) Write(b []byte) (int, error) { return g.writer.Write(b) }

// gzipResponses compresses JSON API responses for clients that accept gzip.
// The static UI files are small and served as they are.
func gzipResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		writer, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer writer.Close()
		next.ServeHTTP(gzipWriter{ResponseWriter: w, writer: writer}, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	page, _ := uiFiles.ReadFile("ui/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"google_maps_api_key": s.config.GoogleMapsAPIKey, "google_maps_map_id": s.config.GoogleMapsMapID,
		"maps_enabled": s.config.GoogleMapsAPIKey != "", "compare_enabled": s.config.Compare != nil,
		"simulation_start_at": s.config.Run.startAt(), "page_size": DefaultPageSize,
		"timezone": s.config.Timezone, "timezone_source": timezoneSource(s.config),
	})
}

// run picks run=a (default) or run=b.
func (s *Server) run(w http.ResponseWriter, r *http.Request) (*Run, bool) {
	if r.URL.Query().Get("run") == "b" {
		if s.config.Compare == nil {
			writeError(w, http.StatusNotFound, "no comparison run; start with -compare")
			return nil, false
		}
		return s.config.Compare, true
	}
	return s.config.Run, true
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if run, ok := s.run(w, r); ok {
		writeJSON(w, http.StatusOK, run.Dashboard())
	}
}

// Page is one page of a long table.
type Page[T any] struct {
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Rows     []T `json:"rows"`
}

func paginate[T any](r *http.Request, rows []T) Page[T] {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 1000 {
		size = DefaultPageSize
	}
	start := min((page-1)*size, len(rows))
	end := min(start+size, len(rows))
	return Page[T]{Total: len(rows), Page: page, PageSize: size, Rows: append([]T{}, rows[start:end]...)}
}

// sortRows sorts by a column, numerically when both cells are numbers.
func sortRows(rows []Row, column, order string) {
	if column == "" {
		return
	}
	less := func(a, b string) bool {
		x, errX := strconv.ParseFloat(a, 64)
		y, errY := strconv.ParseFloat(b, 64)
		if errX == nil && errY == nil {
			return x < y
		}
		if (errX == nil) != (errY == nil) {
			return errX == nil // numbers before empty cells
		}
		return a < b
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if order == "desc" {
			return less(rows[j][column], rows[i][column])
		}
		return less(rows[i][column], rows[j][column])
	})
}

func (s *Server) handleTrips(w http.ResponseWriter, r *http.Request) {
	run, ok := s.run(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	var rows []Row
	for _, trip := range run.Trips {
		keep := true
		for _, field := range []string{"status", "ridepool_type", "cancel_reason", "supply_id"} {
			if want := query.Get(field); want != "" && trip[field] != want {
				keep = false
			}
		}
		if keep {
			rows = append(rows, trip)
		}
	}
	sortRows(rows, query.Get("sort"), query.Get("order"))
	writeJSON(w, http.StatusOK, paginate(r, rows))
}

func (s *Server) handleTrip(w http.ResponseWriter, r *http.Request) {
	run, ok := s.run(w, r)
	if !ok {
		return
	}
	view, found := run.TripView(r.PathValue("request_id"))
	if !found {
		writeError(w, http.StatusNotFound, "unknown request_id")
		return
	}
	if s.config.Compare != nil {
		view.CompareLink = "/compare/trips/" + r.PathValue("request_id")
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSupplyList(w http.ResponseWriter, r *http.Request) {
	run, ok := s.run(w, r)
	if !ok {
		return
	}
	var rows []Row
	for _, supply := range run.SupplySummary {
		row := Row{}
		for k, v := range supply {
			row[k] = v
		}
		row["online_seconds"] = strconv.FormatInt(run.onlineSeconds(supply), 10)
		row["total_meters"] = strconv.FormatInt(totalMeters(supply), 10)
		rows = append(rows, row)
	}
	sortRows(rows, r.URL.Query().Get("sort"), r.URL.Query().Get("order"))
	writeJSON(w, http.StatusOK, paginate(r, rows))
}

func (s *Server) handleSupply(w http.ResponseWriter, r *http.Request) {
	run, ok := s.run(w, r)
	if !ok {
		return
	}
	var at *int64
	if v, err := strconv.ParseInt(r.URL.Query().Get("at"), 10, 64); err == nil {
		at = &v
	}
	view, found := run.SupplyView(r.PathValue("supply_id"), at)
	if !found {
		writeError(w, http.StatusNotFound, "unknown supply_id")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) requireCompare(w http.ResponseWriter) bool {
	if s.config.Compare == nil {
		writeError(w, http.StatusNotFound, "no comparison run; start with -compare")
		return false
	}
	return true
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	if s.requireCompare(w) {
		writeJSON(w, http.StatusOK, s.cachedComparison())
	}
}

func (s *Server) requirePaired(w http.ResponseWriter) bool {
	if !s.requireCompare(w) {
		return false
	}
	c := s.config
	if c.Run.Metadata["requests_sha256"] != c.Compare.Metadata["requests_sha256"] || c.Run.Metadata["supply_sha256"] != c.Compare.Metadata["supply_sha256"] {
		writeError(w, http.StatusConflict, "runs are not paired: requests or supply differ")
		return false
	}
	return true
}

func (s *Server) handleCompareTrips(w http.ResponseWriter, r *http.Request) {
	if !s.requirePaired(w) {
		return
	}
	rows := PairedTrips(s.config.Run, s.config.Compare)
	if r.URL.Query().Get("status_changed") == "true" {
		var changed []PairedTrip
		for _, row := range rows {
			if row.StatusChanged {
				changed = append(changed, row)
			}
		}
		rows = changed
	}
	if column := r.URL.Query().Get("sort"); column != "" {
		pick := map[string]func(PairedTrip) *float64{
			"wait_seconds_delta":         func(p PairedTrip) *float64 { return p.WaitSecondsDelta },
			"trip_distance_meters_delta": func(p PairedTrip) *float64 { return p.TripDistanceMetersDelta },
			"detour_ratio_delta":         func(p PairedTrip) *float64 { return p.DetourRatioDelta },
		}[column]
		if pick != nil {
			descending := r.URL.Query().Get("order") == "desc"
			sort.SliceStable(rows, func(i, j int) bool {
				x, y := pick(rows[i]), pick(rows[j])
				if x == nil || y == nil {
					return x != nil
				}
				if descending {
					return *x > *y
				}
				return *x < *y
			})
		}
	}
	writeJSON(w, http.StatusOK, paginate(r, rows))
}

func (s *Server) handleCompareTrip(w http.ResponseWriter, r *http.Request) {
	if !s.requirePaired(w) {
		return
	}
	id := r.PathValue("request_id")
	a, foundA := s.config.Run.TripView(id)
	b, foundB := s.config.Compare.TripView(id)
	if !foundA || !foundB {
		writeError(w, http.StatusNotFound, "unknown request_id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]TripView{"a": a, "b": b})
}

func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"status": "not_implemented",
		"message": "Matching decisions are TBD; see docs/tooling_spec.md section 5."})
}

func (s *Server) handlePlayback(w http.ResponseWriter, r *http.Request) {
	run, ok := s.run(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	from := run.startAt()
	if v := query.Get("from"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be an integer epoch")
			return
		}
		from = parsed
	}
	to := from + DefaultPlaybackWindowSeconds
	if v := query.Get("to"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to must be an integer epoch")
			return
		}
		to = parsed
	}
	if to < from {
		writeError(w, http.StatusBadRequest, "to is before from")
		return
	}
	if to > from+MaxPlaybackWindowSeconds {
		to = from + MaxPlaybackWindowSeconds
	}
	if end := run.endAt(); to > end && end >= from {
		to = end
	}
	writeJSON(w, http.StatusOK, run.Playback(from, to))
}

func timezoneSource(c Config) string {
	if c.Timezone == "" {
		return TimezoneSourceBrowser
	}
	if c.TimezoneSource == "" {
		return TimezoneSourceFlag
	}
	return c.TimezoneSource
}
