package tests

import (
	"strconv"
	"testing"

	"github.com/realjonathanhester/open-ride-hailing-simulator/runner"
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
	assertEquals(t, second["trip_duration_seconds"], "258")
	assertEquals(t, second["detour_ratio"], "1.000000")

	first := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, first["status"], "completed")
	assertEquals(t, first["pickup_at"], at(60))
	assertEquals(t, first["ridepool_type"], "p0p1d1d0")
	assertEquals(t, first["dropoff_at"], at(715))
	assertEquals(t, first["trip_distance_meters"], "6554")
	assertEquals(t, first["trip_duration_seconds"], "655")
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
