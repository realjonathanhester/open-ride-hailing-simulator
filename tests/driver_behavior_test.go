package tests

import (
	"strconv"
	"testing"
)

// Driver behavior cases 18 to 23 from docs/test_specs.md.

// Case 18: naming the default plugin changes nothing, not even the settings hash.
func TestSimpleDriverBehaviorIsTheDefault(t *testing.T) {
	implicit := simulate(t, "base_default", "one_car_at_union_square", "single_trip")
	explicit := simulate(t, "simple_driver_behavior", "one_car_at_union_square", "single_trip")
	assertFile(t, "trips", explicit.files["trips.csv"], implicit.files["trips.csv"])
	assertFile(t, "supply event log", explicit.files["supply_event_log.csv"], implicit.files["supply_event_log.csv"])
	assertFile(t, "supply summary", explicit.files["supply_summary.csv"], implicit.files["supply_summary.csv"])
	assertFile(t, "run summary", explicit.runSummaryWithoutTimings(), implicit.runSummaryWithoutTimings())
	assertFile(t, "run metadata", explicit.files["run_metadata.csv"], implicit.files["run_metadata.csv"])
}

// Case 19: after the dropoff at Coit Tower the car drives 5940 m (594 s) back
// to Kezar Stadium, counted as idle.
func TestCarReturnsToOriginAfterDropoff(t *testing.T) {
	o := simulate(t, "return_to_origin", "one_car_at_kezar_stadium", "single_trip")
	events := o.events("car_at_kezar_stadium")
	idle, started, arrived := events[len(events)-3], events[len(events)-2], events[len(events)-1]
	assertEquals(t, idle["action"], "idle")
	assertEquals(t, started["action"], "reposition_started")
	assertEquals(t, started["at"], at(654))
	assertEquals(t, started["state_after"], "repositioning")
	assertEquals(t, started["lat"], "37.802395")
	assertEquals(t, started["lng"], "-122.405822")
	assertEquals(t, boolCell(started["planned_polyline"] != ""), "true")
	assertEquals(t, started["plan"], "")
	assertEquals(t, arrived["action"], "reposition_arrived")
	assertEquals(t, arrived["at"], at(1248))
	assertEquals(t, arrived["state_after"], "idle")
	assertEquals(t, arrived["lat"], "37.767263")
	assertEquals(t, arrived["lng"], "-122.456743")
	assertEquals(t, arrived["odometer_meters"], "11880")
	assertEquals(t, boolCell(arrived["driven_polyline"] != ""), "true")

	summary := o.supplySummary("car_at_kezar_stadium")
	assertEquals(t, summary["idle_seconds"], "654")
	assertEquals(t, summary["idle_meters"], "5940")
	assertEquals(t, summary["on_trip_meters"], "5940")
	assertEquals(t, o.runMetadata()["simulation_end_at"], at(1248))
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["dropoff_at"], at(654))
}

// Case 20: at T+954 the car is 3000 m into its return, so the matcher plans
// from there and the pickup at Coit Tower is 300 s and 3000 m away.
func TestCarIsAssignedMidRepositionFromInterpolatedPosition(t *testing.T) {
	o := simulate(t, "return_to_origin", "one_car_at_kezar_stadium", "second_trip_requested_mid_return")
	planUpdated := o.eventsWithAction("car_at_kezar_stadium", "plan_updated")[1]
	assertEquals(t, planUpdated["at"], at(954))
	assertEquals(t, planUpdated["state_after"], "en_route")
	assertEquals(t, planUpdated["lat"], "37.784652")
	assertEquals(t, planUpdated["lng"], "-122.431540")
	assertEquals(t, planUpdated["odometer_meters"], "8940")
	assertPlan(t, planUpdated["plan"], []string{"coit_tower_to_ferry_building.pickup", "coit_tower_to_ferry_building.dropoff"})

	trip := o.trip("coit_tower_to_ferry_building")
	assertEquals(t, trip["eta_at_assignment_seconds"], "300")
	assertEquals(t, trip["pickup_at"], at(1254))
	assertEquals(t, trip["pickup_distance_meters"], "3000")

	// The canceled reposition counts as idle; it starts again after this dropoff.
	assertEquals(t, count(o.eventsWithAction("car_at_kezar_stadium", "reposition_started")), "2")
	assertEquals(t, count(o.eventsWithAction("car_at_kezar_stadium", "reposition_arrived")), "1")
	summary := o.supplySummary("car_at_kezar_stadium")
	assertEquals(t, summary["idle_meters"], "9369")
	assertEquals(t, summary["en_route_meters"], "3000")
}

// Case 21: the second trip ends at Kezar Stadium, so there is nothing to return to.
func TestNoRepositionWhenAlreadyAtOrigin(t *testing.T) {
	o := simulate(t, "return_to_origin", "one_car_at_kezar_stadium", "round_trip_back_to_kezar_stadium")
	events := o.events("car_at_kezar_stadium")
	last := events[len(events)-1]
	assertEquals(t, last["action"], "idle")
	assertEquals(t, last["at"], at(1248))
	assertEquals(t, count(o.eventsWithAction("car_at_kezar_stadium", "reposition_started")), "0")
	assertEquals(t, o.supplySummary("car_at_kezar_stadium")["idle_meters"], "0")
}

// Case 22: offline_at arrives 300 s into the return, so the car stops there.
func TestCarGoesOfflineDuringReposition(t *testing.T) {
	o := simulate(t, "return_to_origin", "one_car_at_kezar_stadium_offline_during_return", "single_trip")
	events := o.events("car_at_kezar_stadium")
	last := events[len(events)-1]
	assertEquals(t, last["action"], "offline")
	assertEquals(t, last["at"], at(954))
	assertEquals(t, last["lat"], "37.784652")
	assertEquals(t, last["odometer_meters"], "8940")
	assertEquals(t, count(o.eventsWithAction("car_at_kezar_stadium", "reposition_arrived")), "0")
	summary := o.supplySummary("car_at_kezar_stadium")
	assertEquals(t, summary["offline_at"], at(954))
	assertEquals(t, summary["idle_meters"], "3000")
}

// Case 23: a car that is going_offline when its plan ends goes offline, not home.
func TestGoingOfflineCarDoesNotReposition(t *testing.T) {
	o := simulate(t, "return_to_origin", "one_car_at_kezar_stadium_offline_early", "single_trip")
	events := o.events("car_at_kezar_stadium")
	last := events[len(events)-1]
	assertEquals(t, last["action"], "offline")
	assertEquals(t, last["at"], at(654))
	assertEquals(t, count(o.eventsWithAction("car_at_kezar_stadium", "reposition_started")), "0")
}

func boolCell(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func count(rows []map[string]string) string { return strconv.Itoa(len(rows)) }
