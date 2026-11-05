package tests

import (
	"strconv"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Case 4 (exhaustive).
func TestSimplePool(t *testing.T) {
	o := simulate(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")

	second := o.trip("painted_ladies_to_union_square")
	assertEquals(t, second["status"], "completed")
	assertEquals(t, second["requested_at"], at(120))
	assertEquals(t, second["first_assigned_at"], at(120))
	assertEquals(t, second["assigned_at"], at(120))
	assertEquals(t, second["supply_id"], "car_at_kezar_stadium")
	assertEquals(t, second["reassign_count"], "0")
	assertEquals(t, second["ridepool_type"], "p0d0")
	assertEquals(t, second["eta_at_assignment_seconds"], "176")
	assertEquals(t, second["driver_arrived_at"], at(296))
	assertEquals(t, second["pickup_at"], at(296))
	assertEquals(t, second["wait_seconds"], "176")
	assertEquals(t, second["pickup_distance_meters"], "1763")
	assertEquals(t, second["dropoff_at"], at(554))
	assertEquals(t, second["trip_distance_meters"], "2583")
	assertEquals(t, second["direct_distance_meters"], "2583")
	assertEquals(t, second["direct_duration_seconds"], "258")
	assertEquals(t, second["max_co_riders"], "1")
	assertEquals(t, second["trip_duration_seconds"], "258")
	assertEquals(t, second["detour_ratio"], "1.000000")

	first := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, first["status"], "completed")
	assertEquals(t, first["pickup_at"], at(60))
	assertEquals(t, first["ridepool_type"], "p0p1d1d0")
	assertEquals(t, first["dropoff_at"], at(715))
	assertEquals(t, first["trip_distance_meters"], "6554")
	assertEquals(t, first["trip_duration_seconds"], "655")
	assertEquals(t, first["direct_duration_seconds"], "594")
	assertEquals(t, first["max_co_riders"], "1")
	assertEquals(t, first["detour_ratio"], strconv.FormatFloat(6554.0/5940.0, 'f', 6, 64))

	// At T+120 the car is 600 m along and picks up the second rider on the way.
	replan := o.eventsWithAction("car_at_kezar_stadium", "plan_updated")[1]
	assertEquals(t, replan["at"], at(120))
	assertEquals(t, replan["odometer_meters"], "600")
	assertPlan(t, replan["plan"], []string{
		"painted_ladies_to_union_square.pickup",
		"painted_ladies_to_union_square.dropoff",
		"kezar_stadium_to_coit_tower.dropoff",
	})
	secondPickup := o.eventsWithAction("car_at_kezar_stadium", "pickup")[1]
	assertEquals(t, secondPickup["seats_in_use"], "2")

	summary := o.supplySummary("car_at_kezar_stadium")
	assertEquals(t, summary["on_trip_meters"], "6554")
	assertEquals(t, summary["requests_served"], "2")

	// Every remaining cell is pinned by the full expected files.
	assertFile(t, "trips", o.files[runner.TripsFile], expectedSimplePoolTrips)
	assertFile(t, "run metadata", o.files[runner.RunMetadataFile], expectedSimplePoolRunMetadata)
	assertFile(t, "supply event log", o.files[runner.SupplyEventsFile], expectedSimplePoolSupplyEventLog)
	assertFile(t, "supply summary", o.files[runner.SupplySummaryFile], expectedSimplePoolSupplySummary)
	assertFile(t, "run summary", o.runSummaryWithoutTimings(), expectedSimplePoolRunSummary)
}

// Case 5.
func TestPoolingOffServesRequestsOneAfterAnother(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium", "simple_pool")
	trip := o.trip("painted_ladies_to_union_square")
	assertEquals(t, trip["ridepool_type"], "d1p0d0")
	assertEquals(t, trip["pickup_at"], at(1030))
	assertEquals(t, trip["max_co_riders"], "0") // forward dispatch, never shared
	for _, event := range o.events("car_at_kezar_stadium") {
		if event["seats_in_use"] != "0" && event["seats_in_use"] != "1" {
			t.Errorf("seats_in_use = %s at %s, want at most 1", event["seats_in_use"], event["at"])
		}
	}
}

// Case 6: 1 + 4 seats exceeds the car's 4.
func TestPartyOfFourCannotPool(t *testing.T) {
	o := simulate(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool_second_rider_party_of_four")
	assertEquals(t, o.trip("painted_ladies_to_union_square")["ridepool_type"], "d1p0d0")
}

// Case 31: once a rider is aboard, a closer car that comes online later does
// not take the trip. Reassignment only considers StatusAssigned (not yet
// arrived); StatusOnTrip is skipped. Contrast case 12, where the same second
// car online before pickup steals the request.
func TestAboardRiderIsNotReassignedWhenCloserCarAppears(t *testing.T) {
	o := simulate(t, "pooling_enabled", "ferry_building_car_then_union_square_car_online_after_pickup", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["supply_id"], "car_at_ferry_building")
	assertEquals(t, trip["reassign_count"], "0")
	assertEquals(t, trip["first_assigned_at"], at(60))
	assertEquals(t, trip["assigned_at"], at(60))
	assertEquals(t, trip["pickup_at"], at(697))
	assertEquals(t, trip["dropoff_at"], at(1291))
	// The late car comes online after pickup and never serves this request.
	for _, event := range o.events("car_at_union_square") {
		if event["action"] == "plan_updated" || event["action"] == "pickup" {
			t.Errorf("closer car online after pickup took action %s at %s; aboard rider must stay with the original car", event["action"], event["at"])
		}
	}
}

// Case 32: a two-seat car already at capacity (two one-seat pooled riders)
// cannot take a third request; that request goes to another open car.
func TestRequestDoesNotPoolOntoCarAtCapacity(t *testing.T) {
	o := simulate(t, "pooling_enabled", "two_seat_car_at_kezar_and_four_seat_car_at_palace", "three_requests_from_kezar")
	first := o.trip("kezar_stadium_to_painted_ladies")
	second := o.trip("kezar_stadium_to_union_square")
	third := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, first["status"], "completed")
	assertEquals(t, second["status"], "completed")
	assertEquals(t, third["status"], "completed")
	assertEquals(t, first["supply_id"], "car_at_kezar_stadium")
	assertEquals(t, second["supply_id"], "car_at_kezar_stadium")
	assertEquals(t, third["supply_id"], "car_at_palace_of_fine_arts")
	assertEquals(t, first["max_co_riders"], "1")
	assertEquals(t, second["max_co_riders"], "1")
	assertEquals(t, third["max_co_riders"], "0")
	assertEquals(t, third["reassign_count"], "0")
	for _, event := range o.events("car_at_kezar_stadium") {
		seats := event["seats_in_use"]
		if seats != "0" && seats != "1" && seats != "2" {
			t.Errorf("seats_in_use = %s at %s, want at most 2 on the two-seat car", seats, event["at"])
		}
	}
}

// Case 16 characterization entry point (same fixtures and asserts as
// TestSameInputsGiveIdenticalOutputs in simple_cases_test.go): two pooling
// runs with identical settings and inputs are byte-identical.
func TestPoolingSameInputsAreDeterministic(t *testing.T) {
	first := simulate(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")
	second := simulate(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")
	assertFile(t, "trips", second.files[runner.TripsFile], first.files[runner.TripsFile])
	assertFile(t, "supply event log", second.files[runner.SupplyEventsFile], first.files[runner.SupplyEventsFile])
	assertFile(t, "supply summary", second.files[runner.SupplySummaryFile], first.files[runner.SupplySummaryFile])
	assertFile(t, "run summary", second.runSummaryWithoutTimings(), first.runSummaryWithoutTimings())
	assertFile(t, "run metadata", second.files[runner.RunMetadataFile], first.files[runner.RunMetadataFile])
}
