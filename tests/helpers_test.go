// Package tests holds the component tests from docs/test_specs.md.
//
// Every test is a component test
// (https://martinfowler.com/articles/microservice-testing/#testing-component):
// it runs a full simulation from one settings TOML, one supply CSV and one
// demand CSV in tests/fixtures, using the real straight-line router and the
// greedy (reference_test_matcher) matcher, then asserts on the output CSVs.
// Tests are grouped into files by behavior; this file holds shared helpers.
package tests

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// T is the base time, 2026-01-01 00:00:00 UTC.
const T = 1767225600

// at returns T plus an offset in seconds, formatted like an output cell.
func at(offsetSeconds int) string { return strconv.Itoa(T + offsetSeconds) }

type outputs struct {
	files map[string]string
	t     *testing.T
}

// simulate runs one fixture combination and checks the invariants every
// trip and supply event log must satisfy.
func simulate(t *testing.T, settings, supply, demand string) outputs {
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
	o := outputs{files: map[string]string{}, t: t}
	for _, name := range []string{runner.TripsFile, runner.SupplyEventsFile, runner.SupplySummaryFile, runner.RunSummaryFile, runner.RunMetadataFile} {
		content, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		o.files[name] = string(content)
	}
	for _, trip := range o.rows(runner.TripsFile) {
		assertValidTrip(t, trip)
	}
	supplyIDs := map[string]bool{}
	for _, event := range o.events("") {
		supplyIDs[event["supply_id"]] = true
	}
	for supplyID := range supplyIDs {
		assertValidSupplyEvents(t, o.events(supplyID))
	}
	return o
}

// rows parses one output file into maps keyed by column name.
func (o outputs) rows(name string) []map[string]string {
	o.t.Helper()
	records, err := csv.NewReader(strings.NewReader(o.files[name])).ReadAll()
	if err != nil {
		o.t.Fatal(err)
	}
	var out []map[string]string
	for _, record := range records[1:] {
		row := map[string]string{}
		for i, column := range records[0] {
			row[column] = record[i]
		}
		out = append(out, row)
	}
	return out
}

func (o outputs) trip(requestID string) map[string]string {
	o.t.Helper()
	for _, row := range o.rows(runner.TripsFile) {
		if row["request_id"] == requestID {
			return row
		}
	}
	o.t.Fatalf("no trip %s", requestID)
	return nil
}

func (o outputs) runMetadata() map[string]string {
	o.t.Helper()
	return o.rows(runner.RunMetadataFile)[0]
}

func (o outputs) supplySummary(supplyID string) map[string]string {
	o.t.Helper()
	for _, row := range o.rows(runner.SupplySummaryFile) {
		if row["supply_id"] == supplyID {
			return row
		}
	}
	o.t.Fatalf("no supply summary for %s", supplyID)
	return nil
}

// events returns the supply event log rows for one car, or all rows for "".
func (o outputs) events(supplyID string) []map[string]string {
	var out []map[string]string
	for _, row := range o.rows(runner.SupplyEventsFile) {
		if supplyID == "" || row["supply_id"] == supplyID {
			out = append(out, row)
		}
	}
	return out
}

// eventsWithAction returns one car's rows with the given action, in order.
func (o outputs) eventsWithAction(supplyID, action string) []map[string]string {
	var out []map[string]string
	for _, row := range o.events(supplyID) {
		if row["action"] == action {
			out = append(out, row)
		}
	}
	return out
}

// runSummaryWithoutTimings drops the two wall-clock timing columns.
func (o outputs) runSummaryWithoutTimings() string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(o.files[runner.RunSummaryFile]), "\n") {
		cells := strings.Split(line, ",")
		lines = append(lines, strings.Join(cells[:len(cells)-2], ","))
	}
	return strings.Join(lines, "\n") + "\n"
}

func assertEquals(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func assertFile(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s differs\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}

// assertPlan checks a plan cell's stops, written as "<request_id>.<type>".
func assertPlan(t *testing.T, planCell string, want []string) {
	t.Helper()
	var stops []struct {
		RequestID string `json:"request_id"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal([]byte(planCell), &stops); err != nil {
		t.Fatalf("plan %q: %v", planCell, err)
	}
	var got []string
	for _, stop := range stops {
		got = append(got, stop.RequestID+"."+stop.Type)
	}
	assertEquals(t, strings.Join(got, " "), strings.Join(want, " "))
}

func number(t *testing.T, cell string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(cell, 10, 64)
	if err != nil {
		t.Fatalf("not an integer: %q", cell)
	}
	return v
}

// assertValidTrip checks invariants that hold for every trip row.
func assertValidTrip(t *testing.T, trip map[string]string) {
	t.Helper()
	id := trip["request_id"]
	switch trip["status"] {
	case "completed":
		requested, firstAssigned, assigned := number(t, trip["requested_at"]), number(t, trip["first_assigned_at"]), number(t, trip["assigned_at"])
		arrived, pickup, dropoff := number(t, trip["driver_arrived_at"]), number(t, trip["pickup_at"]), number(t, trip["dropoff_at"])
		if !(requested <= firstAssigned && firstAssigned <= assigned && assigned <= arrived && arrived <= pickup && pickup <= dropoff) {
			t.Errorf("%s: timestamps out of order", id)
		}
		if number(t, trip["wait_seconds"]) != pickup-requested {
			t.Errorf("%s: wait_seconds != pickup_at - requested_at", id)
		}
		if number(t, trip["trip_duration_seconds"]) != dropoff-pickup {
			t.Errorf("%s: trip_duration_seconds != dropoff_at - pickup_at", id)
		}
		tripMeters, directMeters := number(t, trip["trip_distance_meters"]), number(t, trip["direct_distance_meters"])
		if tripMeters < directMeters {
			t.Errorf("%s: trip_distance_meters is shorter than the direct route", id)
		}
		if directMeters > 0 && trip["detour_ratio"] != strconv.FormatFloat(float64(tripMeters)/float64(directMeters), 'f', 6, 64) {
			t.Errorf("%s: detour_ratio != trip_distance_meters / direct_distance_meters", id)
		}
		if number(t, trip["direct_duration_seconds"]) < 0 || number(t, trip["max_co_riders"]) < 0 {
			t.Errorf("%s: negative direct_duration_seconds or max_co_riders", id)
		}
		ridepool := trip["ridepool_type"]
		if !strings.Contains(ridepool, "p0") || !strings.HasSuffix(ridepool, "d0") {
			t.Errorf("%s: ridepool_type %q must contain p0 and end with d0", id, ridepool)
		}
		if trip["canceled_at"] != "" || trip["cancel_reason"] != "" {
			t.Errorf("%s: a completed trip has cancel fields", id)
		}
	case "canceled":
		if trip["canceled_at"] == "" || trip["cancel_reason"] == "" {
			t.Errorf("%s: a canceled trip needs canceled_at and cancel_reason", id)
		}
		if trip["pickup_at"] != "" || trip["ridepool_type"] != "" {
			t.Errorf("%s: a canceled trip has pickup_at or ridepool_type", id)
		}
	default:
		t.Errorf("%s: unexpected status %q", id, trip["status"])
	}
}

// assertValidSupplyEvents checks invariants that hold for one car's event log.
func assertValidSupplyEvents(t *testing.T, events []map[string]string) {
	t.Helper()
	if len(events) == 0 {
		return
	}
	supplyID := events[0]["supply_id"]
	if events[0]["action"] != "online" {
		t.Errorf("%s: first row is %s, want online", supplyID, events[0]["action"])
	}
	for i, event := range events {
		if number(t, event["seats_in_use"]) < 0 {
			t.Errorf("%s row %d: negative seats_in_use", supplyID, i)
		}
		if (event["action"] == "plan_updated") != (event["plan"] != "") {
			t.Errorf("%s row %d: plan is set only on plan_updated rows", supplyID, i)
		}
		if i == 0 {
			continue
		}
		previous := events[i-1]
		if number(t, event["at"]) < number(t, previous["at"]) {
			t.Errorf("%s row %d: time goes backwards", supplyID, i)
		}
		if number(t, event["odometer_meters"]) < number(t, previous["odometer_meters"]) {
			t.Errorf("%s row %d: odometer goes backwards", supplyID, i)
		}
	}
}
