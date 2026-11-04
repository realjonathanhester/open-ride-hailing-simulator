package tests

import (
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Case 1 (exhaustive).
func TestSingleTripFromCarAtOrigin(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium", "single_trip")

	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["requested_at"], at(60))
	assertEquals(t, trip["num_requested_seats"], "1")
	assertEquals(t, trip["first_assigned_at"], at(60))
	assertEquals(t, trip["assigned_at"], at(60))
	assertEquals(t, trip["supply_id"], "car_at_kezar_stadium")
	assertEquals(t, trip["reassign_count"], "0")
	assertEquals(t, trip["ridepool_type"], "p0d0")
	assertEquals(t, trip["eta_at_assignment_seconds"], "0")
	assertEquals(t, trip["driver_arrived_at"], at(60))
	assertEquals(t, trip["pickup_at"], at(60))
	assertEquals(t, trip["dropoff_at"], at(654))
	assertEquals(t, trip["wait_seconds"], "0")
	assertEquals(t, trip["trip_distance_meters"], "5940")
	assertEquals(t, trip["trip_duration_seconds"], "594")
	assertEquals(t, trip["direct_distance_meters"], "5940")
	assertEquals(t, trip["direct_duration_seconds"], "594")
	assertEquals(t, trip["detour_ratio"], "1.000000")
	assertEquals(t, trip["pickup_distance_meters"], "0")
	assertEquals(t, trip["max_co_riders"], "0")
	assertEquals(t, trip["canceled_at"], "")
	assertEquals(t, trip["cancel_reason"], "")

	events := o.events("car_at_kezar_stadium")
	assertEquals(t, events[0]["action"], "online")
	assertEquals(t, events[0]["at"], at(0))
	assertEquals(t, events[1]["action"], "plan_updated")
	assertEquals(t, events[1]["at"], at(60))
	assertPlan(t, events[1]["plan"], []string{
		"kezar_stadium_to_coit_tower.pickup",
		"kezar_stadium_to_coit_tower.dropoff",
	})
	assertEquals(t, events[2]["action"], "arrived_pickup")
	assertEquals(t, events[3]["action"], "pickup")
	assertEquals(t, events[3]["odometer_meters"], "0")
	assertEquals(t, events[4]["action"], "arrived_dropoff")
	assertEquals(t, events[4]["at"], at(654))
	assertEquals(t, events[4]["odometer_meters"], "5940")
	assertEquals(t, events[5]["action"], "dropoff")
	assertEquals(t, events[6]["action"], "idle")
	assertEquals(t, events[6]["at"], at(654))

	summary := o.supplySummary("car_at_kezar_stadium")
	assertEquals(t, summary["online_at"], at(0))
	assertEquals(t, summary["offline_at"], "")
	assertEquals(t, summary["idle_seconds"], "60")
	assertEquals(t, summary["en_route_seconds"], "0")
	assertEquals(t, summary["on_trip_seconds"], "594")
	assertEquals(t, summary["idle_meters"], "0")
	assertEquals(t, summary["en_route_meters"], "0")
	assertEquals(t, summary["on_trip_meters"], "5940")
	assertEquals(t, summary["requests_served"], "1")
	assertEquals(t, summary["utilization"], "0.908257") // 594 / 654
	assertEquals(t, summary["deadhead_ratio"], "0.000000")

	run := o.rows(runner.RunSummaryFile)[0]
	assertEquals(t, run["requests_completed"], "1")
	assertEquals(t, run["requests_canceled"], "0")
	assertEquals(t, run["median_wait_seconds"], "0")
	assertEquals(t, run["p90_wait_seconds"], "0")
	assertEquals(t, run["mean_detour_ratio"], "1.000000")
	assertEquals(t, run["matcher_calls_request"], "1")
	assertEquals(t, run["matcher_calls_supply_online"], "0")
	assertEquals(t, run["matcher_calls_supply_offline"], "0")
	assertEquals(t, run["matcher_calls_batch_tick"], "0")

	metadata := o.runMetadata()
	assertEquals(t, metadata["simulator_version"], "0.2.0")
	assertEquals(t, metadata["simulation_start_at"], at(60))
	assertEquals(t, metadata["simulation_end_at"], at(654))

	// Every remaining cell (coordinates, polylines, plan ETAs) is pinned by
	// the full expected files.
	assertFile(t, "trips", o.files[runner.TripsFile], expectedSingleTripTrips)
	assertFile(t, "run metadata", o.files[runner.RunMetadataFile], expectedSingleTripRunMetadata)
	assertFile(t, "supply event log", o.files[runner.SupplyEventsFile], expectedSingleTripSupplyEventLog)
	assertFile(t, "supply summary", o.files[runner.SupplySummaryFile], expectedSingleTripSupplySummary)
	assertFile(t, "run summary", o.runSummaryWithoutTimings(), expectedSingleTripRunSummary)
}

// Case 2.
func TestIdleCarDrivesToPickup(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_union_square", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["pickup_at"], at(551))
	assertEquals(t, trip["wait_seconds"], "491")
	assertEquals(t, trip["pickup_distance_meters"], "4908")
}

// Case 3.
func TestSimpleForwardDispatch(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium", "simple_forward_dispatch")
	trip := o.trip("coit_tower_to_ferry_building")
	assertEquals(t, trip["ridepool_type"], "d1p0d0")
	assertEquals(t, trip["pickup_at"], at(654))

	// At T+654 the first rider gets out before the second gets in.
	dropoff := o.eventsWithAction("car_at_kezar_stadium", "dropoff")[0]
	pickup := o.eventsWithAction("car_at_kezar_stadium", "pickup")[1]
	assertEquals(t, dropoff["at"], at(654))
	assertEquals(t, dropoff["request_id"], "kezar_stadium_to_coit_tower")
	assertEquals(t, pickup["at"], at(654))
	assertEquals(t, pickup["request_id"], "coit_tower_to_ferry_building")
}

// Case 10.
func TestDwellTimesDelayPickupAndDropoff(t *testing.T) {
	o := simulate(t, "with_dwell_times", "one_car_at_kezar_stadium", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["pickup_at"], at(90))
	assertEquals(t, trip["dropoff_at"], at(704))
	assertEquals(t, trip["trip_duration_seconds"], "614") // 594 driving + 20 s dropoff dwell
}

// Case 11.
func TestCarGoesOfflineAfterFinishingPlan(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium_offline_early", "single_trip")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["status"], "completed")

	events := o.events("car_at_kezar_stadium")
	secondToLast, last := events[len(events)-2], events[len(events)-1]
	assertEquals(t, secondToLast["action"], "dropoff")
	assertEquals(t, last["action"], "offline")
	assertEquals(t, last["at"], at(654))
}

// Case 15.
func TestSupplyOnlineRunsBeforeRequestAtSameSecond(t *testing.T) {
	o := simulate(t, "base_default", "one_car_online_with_first_request_at_kezar_stadium", "single_trip")
	events := o.events("car_at_kezar_stadium")
	assertEquals(t, events[0]["action"], "online")
	assertEquals(t, events[0]["at"], at(60))
	assertEquals(t, events[1]["action"], "plan_updated")
	assertEquals(t, events[1]["at"], at(60))
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["wait_seconds"], "0")
}

// Case 16.
func TestSameInputsGiveIdenticalOutputs(t *testing.T) {
	first := simulate(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")
	second := simulate(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")
	assertFile(t, "trips", second.files[runner.TripsFile], first.files[runner.TripsFile])
	assertFile(t, "supply event log", second.files[runner.SupplyEventsFile], first.files[runner.SupplyEventsFile])
	assertFile(t, "supply summary", second.files[runner.SupplySummaryFile], first.files[runner.SupplySummaryFile])
	assertFile(t, "run summary", second.runSummaryWithoutTimings(), first.runSummaryWithoutTimings())
	assertFile(t, "run metadata", second.files[runner.RunMetadataFile], first.files[runner.RunMetadataFile])
}

// Same requests and supply with different settings: the input hashes match
// (the runs are paired) and only the settings hash differs.
func TestInputHashesPairRunsWithDifferentSettings(t *testing.T) {
	pooled := simulate(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool").runMetadata()
	unpooled := simulate(t, "base_default", "one_car_at_kezar_stadium", "simple_pool").runMetadata()
	assertEquals(t, unpooled["requests_sha256"], pooled["requests_sha256"])
	assertEquals(t, unpooled["supply_sha256"], pooled["supply_sha256"])
	if unpooled["settings_sha256"] == pooled["settings_sha256"] {
		t.Errorf("settings_sha256 should differ when pooling_enabled differs")
	}
}
