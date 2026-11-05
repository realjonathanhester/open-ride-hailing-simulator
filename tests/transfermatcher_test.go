package tests

import (
	"encoding/csv"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Case 46: transfermatcher with handoffs disabled is byte-identical to greedy.
func TestTransferMatcherHandoffsOffMatchesGreedy(t *testing.T) {
	transfer := simulate(t, "transfermatcher_handoffs_off", "kezar_and_union_square_cars", "simple_pool")
	greedy := simulate(t, "transfermatcher_greedy", "kezar_and_union_square_cars", "simple_pool")
	for _, name := range []string{runner.TripsFile, runner.SupplyEventsFile, runner.SupplySummaryFile} {
		assertEquals(t, transfer.files[name], greedy.files[name])
	}
}

// Case 46b: transfers_enabled=false with handoffs on matches greedy on pooling+reassignment.
func TestTransferMatcherTransfersOffMatchesGreedyPooling(t *testing.T) {
	transfer := simulate(t, "transfermatcher_transfers_off", "kezar_and_union_square_cars", "simple_pool")
	greedy := simulate(t, "greedy_handoffs_on", "kezar_and_union_square_cars", "simple_pool")
	for _, name := range []string{runner.TripsFile, runner.SupplyEventsFile, runner.SupplySummaryFile} {
		assertEquals(t, transfer.files[name], greedy.files[name])
	}
}

// Case 47: treatment metadata records ratio, budget, transfers_enabled and matcher.
func TestTransferMatcherMetadataRecordsRatioAndMatcher(t *testing.T) {
	o := simulate(t, "transfermatcher_treatment", "kezar_and_union_square_cars", "single_trip")
	settingsJSON := o.runMetadata()["settings"]
	var settings map[string]any
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["matching_hook"] != "transfermatcher" {
		t.Fatalf("matching_hook = %v", settings["matching_hook"])
	}
	if settings["max_rider_path_ratio"] != 1.25 {
		t.Fatalf("max_rider_path_ratio = %v", settings["max_rider_path_ratio"])
	}
	if settings["max_insertion_evaluations"] != float64(5000) {
		t.Fatalf("max_insertion_evaluations = %v", settings["max_insertion_evaluations"])
	}
	if settings["transfers_enabled"] != true {
		t.Fatalf("transfers_enabled = %v", settings["transfers_enabled"])
	}
	if settings["max_handoff_wait_seconds"] != float64(300) {
		t.Fatalf("max_handoff_wait_seconds = %v", settings["max_handoff_wait_seconds"])
	}
}

// Case 48: same inputs twice → byte-identical trips/legs/events.
func TestTransferMatcherDeterministicOutputs(t *testing.T) {
	a := simulate(t, "transfermatcher_treatment", "transfer_mid_corridor_cars", "transfer_win_mid_corridor")
	b := simulate(t, "transfermatcher_treatment", "transfer_mid_corridor_cars", "transfer_win_mid_corridor")
	for _, name := range []string{runner.TripsFile, runner.SupplyEventsFile} {
		assertEquals(t, a.files[name], b.files[name])
	}
	assertEquals(t, a.files[runner.TripLegsFile], b.files[runner.TripLegsFile])
}

// Case 49: tiny insertion budget is a hard ceiling; greedy may still assign.
func TestTransferMatcherBudgetSkipsTransfer(t *testing.T) {
	o := simulate(t, "transfermatcher_budget_1", "transfer_three_cars", "transfer_single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	if trip["status"] != "completed" && trip["status"] != "canceled" {
		t.Fatalf("status = %s", trip["status"])
	}
	if trip["handoff_count"] != "0" && trip["handoff_count"] != "" {
		if trip["status"] == "completed" && trip["handoff_count"] != "0" {
			// With budget 1 a transfer should not be searchable; allow 0 only.
			t.Fatalf("handoff_count = %s, want 0 under budget 1", trip["handoff_count"])
		}
	}
}

// Case 50: matcher view RealizedAboardDistanceMeters equals completed-leg sum at
// live handoff and is zero before the handoff dropoff.
func TestRealizedAboardDistanceExposedToMatcher(t *testing.T) {
	captureRealized.reset()
	o := simulate(t, "capture_realized_distance", "kezar_and_union_square_cars", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["handoff_count"], "1")
	legs := o.rows(runner.TripLegsFile)
	if len(legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(legs))
	}
	firstLeg, err := strconv.ParseInt(legs[0]["distance_meters"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if captureRealized.beforeHandoff != 0 {
		t.Fatalf("pre-dropoff RealizedAboardDistanceMeters = %d, want 0", captureRealized.beforeHandoff)
	}
	if captureRealized.afterHandoff != firstLeg {
		t.Fatalf("post-dropoff RealizedAboardDistanceMeters = %d, want first-leg %d", captureRealized.afterHandoff, firstLeg)
	}
}

// Case 51: deterministic straight-line transfer wins end-to-end.
func TestTransferMatcherStraightLineTransferWins(t *testing.T) {
	o := simulate(t, "transfermatcher_treatment", "transfer_mid_corridor_cars", "transfer_win_mid_corridor")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["handoff_count"], "1")
	legs := o.rows(runner.TripLegsFile)
	var transferLegs []map[string]string
	for _, leg := range legs {
		if leg["request_id"] == "kezar_stadium_to_coit_tower" {
			transferLegs = append(transferLegs, leg)
		}
	}
	if len(transferLegs) != 2 {
		t.Fatalf("transfer trip_legs = %d, want 2; all legs=%v", len(transferLegs), legs)
	}
	first, _ := strconv.ParseInt(transferLegs[0]["distance_meters"], 10, 64)
	second, _ := strconv.ParseInt(transferLegs[1]["distance_meters"], 10, 64)
	// OD via the same straight-line router distance the matcher uses for the cap.
	od := haversineFixtureOD()
	cap := int64(math.Floor(1.25 * float64(od)))
	if first+second > cap {
		t.Fatalf("realized+second %d+%d=%d exceeds floor(1.25*OD)=%d", first, second, first+second, cap)
	}
	firstCar := transferLegs[0]["supply_id"]
	secondCar := transferLegs[1]["supply_id"]
	var handoffAt string
	for _, ev := range o.eventsWithAction(firstCar, "dropoff") {
		if ev["request_id"] == "kezar_stadium_to_coit_tower" {
			handoffAt = ev["at"]
			break
		}
	}
	if handoffAt == "" {
		t.Fatalf("missing handoff dropoff on first-leg car %s", firstCar)
	}
	// Second-leg pickup must be at/after the handoff dropoff. When A and B are the
	// same car, the first pickup action is leg 0 — skip it.
	pickups := 0
	for _, ev := range o.events(secondCar) {
		if ev["request_id"] != "kezar_stadium_to_coit_tower" || ev["action"] != "pickup" {
			continue
		}
		pickups++
		if firstCar == secondCar && pickups == 1 {
			continue
		}
		if ev["at"] < handoffAt {
			t.Fatalf("second-leg pickup before handoff: %+v (handoffAt=%s)", ev, handoffAt)
		}
	}
	if pickups == 0 || (firstCar == secondCar && pickups < 2) {
		t.Fatalf("missing second-leg pickup on %s (pickups=%d)", secondCar, pickups)
	}
}

// Case 52 / 53: Comparison-B control/treatment settings hygiene.
func TestComparisonBControlSettingsOmitTransferFields(t *testing.T) {
	directory := t.TempDir()
	_, err := runner.RunFiles(
		filepath.Join("..", "experiments", "sweep", "examples", "comparison_b", "settings_control.toml"),
		runner.Paths{
			Supply:          filepath.Join("fixtures", "supply", "kezar_and_union_square_cars.csv"),
			Requests:        filepath.Join("fixtures", "demand", "single_trip.csv"),
			OutputDirectory: directory,
		},
	)
	if err != nil {
		t.Fatalf("control run failed: %v", err)
	}
	settingsJSON, err := settingsFromMetadata(filepath.Join(directory, runner.RunMetadataFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"max_rider_path_ratio", "handoffs_enabled", "transfers_enabled"} {
		if strings.Contains(settingsJSON, key) {
			t.Fatalf("control settings must omit %s: %s", key, settingsJSON)
		}
	}
	if !strings.Contains(settingsJSON, `"matching_hook":"greedy_nearest_eta"`) {
		t.Fatalf("control matching_hook: %s", settingsJSON)
	}
}

func TestComparisonBTreatmentSettingsRecordMatcherConfig(t *testing.T) {
	directory := t.TempDir()
	_, err := runner.RunFiles(
		filepath.Join("..", "experiments", "sweep", "examples", "comparison_b", "settings_treatment.toml"),
		runner.Paths{
			Supply:          filepath.Join("fixtures", "supply", "kezar_and_union_square_cars.csv"),
			Requests:        filepath.Join("fixtures", "demand", "single_trip.csv"),
			OutputDirectory: directory,
		},
	)
	if err != nil {
		t.Fatalf("treatment run failed: %v", err)
	}
	settingsJSON, err := settingsFromMetadata(filepath.Join(directory, runner.RunMetadataFile))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["matching_hook"] != "transfermatcher" {
		t.Fatalf("matching_hook = %v", settings["matching_hook"])
	}
	if settings["transfers_enabled"] != true {
		t.Fatalf("transfers_enabled = %v", settings["transfers_enabled"])
	}
	if settings["max_rider_path_ratio"] != 1.25 {
		t.Fatalf("max_rider_path_ratio = %v", settings["max_rider_path_ratio"])
	}
	if settings["max_insertion_evaluations"] != float64(5000) {
		t.Fatalf("max_insertion_evaluations = %v", settings["max_insertion_evaluations"])
	}
	if settings["max_handoff_wait_seconds"] != float64(300) {
		t.Fatalf("max_handoff_wait_seconds = %v", settings["max_handoff_wait_seconds"])
	}
}

func settingsFromMetadata(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return "", err
	}
	if len(records) < 2 {
		return "", os.ErrNotExist
	}
	for i, column := range records[0] {
		if column == "settings" {
			return records[1][i], nil
		}
	}
	return "", os.ErrNotExist
}

func haversineFixtureOD() int64 {
	// Matches routing.HaversineMeters for Kezar→Coit used by straight_line.
	o := model.Location{Lat: 37.767263, Lng: -122.456743}
	d := model.Location{Lat: 37.802395, Lng: -122.405822}
	return fixtureHaversine(o, d)
}

func fixtureHaversine(a, b model.Location) int64 {
	const earth = 6371008.8
	toRad := math.Pi / 180
	dLat := (b.Lat - a.Lat) * toRad
	dLng := (b.Lng - a.Lng) * toRad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*toRad)*math.Cos(b.Lat*toRad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return int64(math.Round(2 * earth * math.Asin(math.Min(1, math.Sqrt(h)))))
}
