package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
	"github.com/realjonathanhester/ride-hailing-simulator/viewer"
)

// Viewer component tests: run the simulator on fixtures, serve the outputs
// with rhs view's HTTP handler and assert on the JSON endpoints.

func simulateForViewer(t *testing.T, settings, supply, demand string) *viewer.Run {
	t.Helper()
	directory := t.TempDir()
	_, err := runner.RunFiles(filepath.Join("fixtures", "settings", settings+".toml"), runner.Paths{
		Supply:          filepath.Join("fixtures", "supply", supply+".csv"),
		Requests:        filepath.Join("fixtures", "demand", demand+".csv"),
		OutputDirectory: directory,
	})
	if err != nil {
		t.Fatalf("simulation failed: %v", err)
	}
	run, err := viewer.LoadRun(viewer.CSVTableReader{Directory: directory})
	if err != nil {
		t.Fatalf("loading outputs failed: %v", err)
	}
	return run
}

func serve(t *testing.T, config viewer.Config) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(viewer.NewServer(config))
	t.Cleanup(server.Close)
	return server
}

// get fetches a URL and decodes JSON into out (when non-nil); returns the status.
func get(t *testing.T, url string, out any) int {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if out != nil && strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(body, out); err != nil {
			t.Fatalf("%s: %v", url, err)
		}
	}
	return response.StatusCode
}

type dashboardResponse struct {
	Metrics []struct {
		Name  string   `json:"name"`
		Value *float64 `json:"value"`
	} `json:"metrics"`
	RidepoolTypes []struct {
		RidepoolType string `json:"ridepool_type"`
		Count        int    `json:"count"`
	} `json:"ridepool_types"`
}

func (d dashboardResponse) metric(t *testing.T, name string) string {
	t.Helper()
	for _, m := range d.Metrics {
		if m.Name == name {
			if m.Value == nil {
				return "n/a"
			}
			return formatFloat(*m.Value)
		}
	}
	t.Fatalf("no metric %s", name)
	return ""
}

func formatFloat(v float64) string {
	encoded, _ := json.Marshal(v)
	return string(encoded)
}

func ratioString(numerator, denominator float64) string { return formatFloat(numerator / denominator) }

// Case 1 through the dashboard.
func TestDashboardForSingleTrip(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "base_default", "one_car_at_kezar_stadium", "single_trip")})
	var dashboard dashboardResponse
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/api/dashboard", &dashboard)), "200")
	assertEquals(t, dashboard.metric(t, "on_trip_time_share"), ratioString(594, 654))
	assertEquals(t, dashboard.metric(t, "idle_time_share"), ratioString(60, 654))
	assertEquals(t, dashboard.metric(t, "wait_seconds_median"), "0")
	assertEquals(t, dashboard.metric(t, "completion_rate"), "1")
	assertEquals(t, dashboard.metric(t, "cancel_after_assignment_share"), "n/a") // no canceled trips
}

// Case 4 through the dashboard.
func TestDashboardForSimplePool(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")})
	var dashboard dashboardResponse
	get(t, server.URL+"/api/dashboard", &dashboard)
	assertEquals(t, dashboard.metric(t, "shared_trip_rate"), "1")
	assertEquals(t, dashboard.metric(t, "rider_meters_per_supply_meter"), ratioString(5940+2583, 6554))
	assertEquals(t, dashboard.metric(t, "extra_meters_p90"), "614") // 6554 - 5940
	assertEquals(t, dashboard.metric(t, "wait_seconds_p90"), "176")
	assertEquals(t, dashboard.RidepoolTypes[0].RidepoolType, "p0d0") // ties sort by name
}

func TestTripViewLabelsCoRiders(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")})
	var trip struct {
		Shared   bool `json:"shared"`
		Ridepool []struct {
			Token     string `json:"token"`
			RequestID string `json:"request_id"`
			Link      string `json:"link"`
		} `json:"ridepool"`
		SupplyLink string `json:"supply_link"`
	}
	get(t, server.URL+"/api/trips/kezar_stadium_to_coit_tower", &trip)
	assertEquals(t, strconv.Itoa(len(trip.Ridepool)), "4")
	second := trip.Ridepool[1]
	assertEquals(t, second.Token, "p1")
	assertEquals(t, second.Link, "/trips/painted_ladies_to_union_square")
	assertEquals(t, trip.SupplyLink, "/supply/car_at_kezar_stadium?at="+at(60))
}

func TestSupplyViewColorsPathByState(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "base_default", "one_car_at_union_square", "single_trip")})
	var supply struct {
		MaxRidersAboard int `json:"max_riders_aboard"`
		Segments        []struct {
			State string `json:"state"`
		} `json:"segments"`
		Trips []struct {
			Trip map[string]string `json:"trip"`
		} `json:"trips"`
		PositionAt *struct {
			Lat float64 `json:"lat"`
		} `json:"position_at"`
	}
	get(t, server.URL+"/api/supply/car_at_union_square?at="+at(300), &supply)
	assertEquals(t, supply.Segments[0].State, "en_route") // Union Square to Kezar Stadium
	assertEquals(t, supply.Segments[1].State, "on_trip")  // Kezar Stadium to Coit Tower
	assertEquals(t, strconv.Itoa(supply.MaxRidersAboard), "1")
	assertEquals(t, supply.Trips[0].Trip["wait_seconds"], "491") // trip info for map popups
	if supply.PositionAt == nil {
		t.Errorf("position_at missing for ?at=")
	}
}

func TestTripListFiltersAndPaginates(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "base_default", "one_car_at_kezar_stadium", "simple_pool")})
	var page struct {
		Total int                 `json:"total"`
		Rows  []map[string]string `json:"rows"`
	}
	get(t, server.URL+"/api/trips?ridepool_type=d1p0d0&page_size=1", &page)
	assertEquals(t, strconv.Itoa(page.Total), "1")
	assertEquals(t, page.Rows[0]["request_id"], "painted_ladies_to_union_square")
}

func TestCompareShowsPairedRunsAndSettingsDiff(t *testing.T) {
	server := serve(t, viewer.Config{
		Run:     simulateForViewer(t, "base_default", "one_car_at_kezar_stadium", "simple_pool"),
		Compare: simulateForViewer(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool"),
	})
	var comparison struct {
		Paired       bool `json:"paired"`
		SettingsDiff []struct {
			Name string `json:"name"`
		} `json:"settings_diff"`
		Metrics []struct {
			Name    string   `json:"name"`
			Delta   *float64 `json:"delta"`
			Verdict string   `json:"verdict"`
		} `json:"metrics"`
	}
	get(t, server.URL+"/api/compare", &comparison)
	if !comparison.Paired {
		t.Errorf("runs with the same requests and supply should be paired")
	}
	assertEquals(t, comparison.SettingsDiff[0].Name, "pooling_enabled")
	for _, m := range comparison.Metrics {
		if m.Name == "wait_seconds_p90" {
			assertEquals(t, formatFloat(*m.Delta), "-734") // 910 without pooling, 176 with
			assertEquals(t, m.Verdict, "better")
		}
	}
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/api/compare/trips", nil)), "200")
}

func TestCompareRefusesPerTripViewWhenNotPaired(t *testing.T) {
	server := serve(t, viewer.Config{
		Run:     simulateForViewer(t, "base_default", "one_car_at_kezar_stadium", "single_trip"),
		Compare: simulateForViewer(t, "base_default", "one_car_at_kezar_stadium", "simple_pool"),
	})
	var comparison struct {
		Paired bool `json:"paired"`
	}
	get(t, server.URL+"/api/compare", &comparison)
	if comparison.Paired {
		t.Errorf("runs with different requests should not be paired")
	}
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/api/compare/trips", nil)), "409")
}

func TestPagesWorkWithoutMapsKey(t *testing.T) {
	t.Setenv("GOOGLE_MAPS_API_KEY", "")
	maps, err := viewer.LoadMapsSettings(filepath.Join(t.TempDir(), "missing_viewer.toml"))
	if err != nil {
		t.Fatal(err)
	}
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "base_default", "one_car_at_kezar_stadium", "single_trip"), GoogleMapsAPIKey: maps.GoogleMapsAPIKey})
	var config struct {
		MapsEnabled bool `json:"maps_enabled"`
	}
	get(t, server.URL+"/api/config", &config)
	if config.MapsEnabled {
		t.Errorf("maps should be disabled without a key")
	}
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/trips/kezar_stadium_to_coit_tower", nil)), "200")
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/api/decisions", nil)), "501") // placeholder
}
