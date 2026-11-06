package tests

import (
	"strconv"
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/routing"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
	"github.com/realjonathanhester/ride-hailing-simulator/simulator"
	"github.com/realjonathanhester/ride-hailing-simulator/viewer"
)

// [simulation] stop_when_requests_resolved (default true): the run ends as
// soon as every request has arrived and is completed or canceled, and every
// car still online goes offline then.

// assertStoppedAtLastResolution checks the early-stop invariants: the run
// ends at the last dropoff or cancellation, and every car that came online
// went offline no later than that, with an offline row closing its log.
func assertStoppedAtLastResolution(t *testing.T, o outputs) {
	t.Helper()
	var last int64
	for _, trip := range o.rows(runner.TripsFile) {
		if trip["status"] != "completed" && trip["status"] != "canceled" {
			t.Fatalf("trip %s ended %s", trip["request_id"], trip["status"])
		}
		for _, column := range []string{"dropoff_at", "canceled_at"} {
			if v, err := strconv.ParseInt(trip[column], 10, 64); err == nil && v > last {
				last = v
			}
		}
	}
	end := o.runMetadata()["simulation_end_at"]
	assertEquals(t, end, strconv.FormatInt(last, 10))
	for _, summary := range o.rows(runner.SupplySummaryFile) {
		if summary["online_at"] == "" {
			continue
		}
		if summary["offline_at"] == "" || summary["offline_at"] > end {
			t.Errorf("%s offline_at = %q, want at or before %s", summary["supply_id"], summary["offline_at"], end)
		}
		events := o.events(summary["supply_id"])
		lastEvent := events[len(events)-1]
		assertEquals(t, lastEvent["action"], "offline")
		assertEquals(t, lastEvent["at"], summary["offline_at"])
	}
}

func TestEarlyStopEndsAtLastDropoff(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium", "single_trip")
	assertStoppedAtLastResolution(t, o)
	assertEquals(t, o.runMetadata()["simulation_end_at"], at(654))
	if !strings.Contains(o.runMetadata()["settings"], `"stop_when_requests_resolved":true`) {
		t.Errorf("settings JSON does not record stop_when_requests_resolved: %s", o.runMetadata()["settings"])
	}
}

// A request arriving after a lull keeps the run going: the car is idle with
// nothing to do between the first dropoff (T+654) and the second request
// (T+954), but the run must not stop there.
func TestEarlyStopWaitsForFutureRequests(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium", "second_trip_requested_mid_return")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["status"], "completed")
	second := o.trip("coit_tower_to_ferry_building")
	assertEquals(t, second["status"], "completed")
	assertStoppedAtLastResolution(t, o)
	assertEquals(t, o.runMetadata()["simulation_end_at"], second["dropoff_at"])
}

// A repositioning car is cut off where it is when the run ends.
func TestEarlyStopClosesOutRepositioningCar(t *testing.T) {
	o := simulate(t, "return_to_origin_stop_early", "one_car_at_kezar_stadium", "single_trip")
	assertStoppedAtLastResolution(t, o)
	summary := o.supplySummary("car_at_kezar_stadium")
	assertEquals(t, summary["offline_at"], at(654))
	assertEquals(t, summary["idle_meters"], "0")
	assertEquals(t, summary["on_trip_meters"], "5940")
}

// Reassignment in flight: the request is moved to a car that comes online
// later; the run waits for that trip.
func TestEarlyStopWithReassignment(t *testing.T) {
	o := simulate(t, "base_default", "ferry_building_car_then_union_square_car_online_late", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["reassign_count"], "1")
	assertStoppedAtLastResolution(t, o)
}

// A rider waiting_for_handoff is not resolved: the run continues until the
// onward car completes the trip.
func TestEarlyStopWaitsForHandoffRider(t *testing.T) {
	o := simulate(t, "handoffs_enabled", "kezar_and_union_square_cars", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["handoff_count"], "1")
	assertStoppedAtLastResolution(t, o)
	legs := o.rows(runner.TripLegsFile)
	assertEquals(t, o.runMetadata()["simulation_end_at"], legs[len(legs)-1]["dropoff_at"])
}

func TestEarlyStopRunsLoadInViewer(t *testing.T) {
	run := simulateForViewer(t, "base_default", "one_car_at_union_square", "single_trip")
	server := serve(t, viewer.Config{Run: run})
	var playback struct {
		SimulationEndAt int64            `json:"simulation_end_at"`
		Cars            []map[string]any `json:"cars"`
	}
	get(t, server.URL+"/api/playback?from="+at(0)+"&to="+at(3600), &playback)
	assertEquals(t, strconv.FormatInt(playback.SimulationEndAt, 10), at(551+594))
	if len(playback.Cars) != 1 {
		t.Fatalf("playback cars = %d, want 1", len(playback.Cars))
	}
	var supply struct {
		Segments []struct {
			State string `json:"state"`
		} `json:"segments"`
	}
	get(t, server.URL+"/api/supply/car_at_union_square?at="+at(300), &supply)
	assertEquals(t, supply.Segments[0].State, "en_route")
	assertEquals(t, supply.Segments[1].State, "on_trip")
}

// No requests is still an input error with either setting.
func TestEarlyStopNoRequests(t *testing.T) {
	for _, stop := range []bool{true, false} {
		settings := model.Settings{DefaultNumRequestedSeats: 1, DefaultNumAvailableSeats: 4, AverageSpeedMetersPerSecond: 10, StopWhenRequestsResolved: stop}
		supply := []model.Supply{{SupplyID: "car", OnlineAt: T, OnlineLocation: unionSquare}}
		router, err := routing.NewStraightLine(10)
		if err != nil {
			t.Fatal(err)
		}
		sim := simulator.New(settings, router, matching.NewGreedy())
		if _, err := sim.Run(nil, supply); err == nil || !strings.Contains(err.Error(), "no ride requests") {
			t.Errorf("stop=%v: err = %v, want no ride requests", stop, err)
		}
	}
}
