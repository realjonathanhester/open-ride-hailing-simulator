package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/routing"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// fakeOSRMServer answers /table with straight-line distances at 10 m/s, so
// a run through the OSRM router matches the straight-line fixtures, and
// /route with an empty road shape.
func fakeOSRMServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var points []model.Location
		path := strings.Split(r.URL.Path, "/")
		for _, c := range strings.Split(path[len(path)-1], ";") {
			lng, lat, _ := strings.Cut(c, ",")
			x, _ := strconv.ParseFloat(lng, 64)
			y, _ := strconv.ParseFloat(lat, 64)
			points = append(points, model.Location{Lat: y, Lng: x})
		}
		params := map[string][]int{}
		for _, kv := range strings.Split(r.URL.RawQuery, "&") {
			k, v, _ := strings.Cut(kv, "=")
			for _, s := range strings.Split(v, ";") {
				i, _ := strconv.Atoi(s)
				params[k] = append(params[k], i)
			}
		}
		body := map[string]any{"code": "Ok", "routes": []any{map[string]any{"geometry": ""}}}
		if strings.HasPrefix(r.URL.Path, "/table/") {
			var durations, distances [][]float64
			for _, i := range params["sources"] {
				var du, di []float64
				for _, j := range params["destinations"] {
					meters := float64(routing.HaversineMeters(points[i], points[j]))
					du, di = append(du, meters/10), append(di, meters)
				}
				durations, distances = append(durations, du), append(distances, di)
			}
			body["durations"], body["distances"] = durations, distances
		}
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return server
}

// osrmSettings writes base_default.toml with [hooks] routing = "osrm" and
// the given [osrm] section.
func osrmSettings(t *testing.T, osrmSection string) string {
	t.Helper()
	base, err := os.ReadFile(filepath.Join("fixtures", "settings", "base_default.toml"))
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Replace(string(base), `routing = "straight_line"`, `routing = "osrm"`, 1) + "\n[osrm]\n" + osrmSection
	path := filepath.Join(t.TempDir(), "osrm.toml")
	os.WriteFile(path, []byte(content), 0o644)
	return path
}

func simulateWithSettingsFile(t *testing.T, settingsPath, supply, demand string) map[string]string {
	t.Helper()
	directory := t.TempDir()
	_, err := runner.RunFiles(settingsPath, runner.Paths{
		Supply:          filepath.Join("fixtures", "supply", supply+".csv"),
		Requests:        filepath.Join("fixtures", "demand", demand+".csv"),
		OutputDirectory: directory,
	})
	if err != nil {
		t.Fatalf("simulation failed: %v", err)
	}
	o := outputs{files: map[string]string{}, t: t}
	for _, name := range []string{runner.TripsFile, runner.RunMetadataFile} {
		content, _ := os.ReadFile(filepath.Join(directory, name))
		o.files[name] = string(content)
	}
	trip := o.trip("kezar_stadium_to_coit_tower")
	trip["settings"] = o.runMetadata()["settings"]
	return trip
}

func TestOSRMRouterRunsTheSingleTripCase(t *testing.T) {
	server := fakeOSRMServer(t)
	settings := osrmSettings(t, "url = \""+server.URL+"\"\n"+`dataset_name = "test map"`+"\n"+`dataset_sha256 = "abc"`+"\n")

	trip := simulateWithSettingsFile(t, settings, "one_car_at_kezar_stadium", "single_trip")

	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["dropoff_at"], at(654))
	assertEquals(t, trip["trip_distance_meters"], "5940")
	assertEquals(t, trip["trip_duration_seconds"], "594")
	assertEquals(t, strconv.FormatBool(strings.Contains(trip["settings"], `"routing_hook":"osrm"`)), "true")
	assertEquals(t, strconv.FormatBool(strings.Contains(trip["settings"], `"router":{"name":"osrm","profile":"car","dataset_name":"test map","dataset_sha256":"abc"}`)), "true")
}

func TestStraightLineRunMetadataHasNoRouterField(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium", "single_trip")

	assertEquals(t, strconv.FormatBool(strings.Contains(o.runMetadata()["settings"], `"router"`)), "false")
}

func TestOSRMRouterWithoutAServerFailsFast(t *testing.T) {
	server := fakeOSRMServer(t)
	server.Close()
	settings := osrmSettings(t, "url = \""+server.URL+"\"\n")

	_, err := runner.RunFiles(settings, runner.Paths{
		Supply:          filepath.Join("fixtures", "supply", "one_car_at_kezar_stadium.csv"),
		Requests:        filepath.Join("fixtures", "demand", "single_trip.csv"),
		OutputDirectory: t.TempDir(),
	})

	assertEquals(t, strconv.FormatBool(err != nil && strings.Contains(err.Error(), "no OSRM server")), "true")
}
