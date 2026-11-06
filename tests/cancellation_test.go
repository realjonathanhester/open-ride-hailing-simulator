package tests

import "testing"

// Case 7.
func TestRiderCancelsWhenNoCarIsOnline(t *testing.T) {
	o := simulate(t, "short_assignment_timeout", "one_car_online_late_at_kezar_stadium", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "canceled")
	assertEquals(t, trip["cancel_reason"], "max_request_to_assignment")
	assertEquals(t, trip["canceled_at"], at(180))
}

// Case 8.
func TestRiderCancelsWhenPickupTakesTooLong(t *testing.T) {
	o := simulate(t, "short_pickup_timeout", "one_car_at_ferry_building", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["cancel_reason"], "max_request_to_pickup")
	assertEquals(t, trip["canceled_at"], at(360))

	// The car's last rows: it drops the canceled stop and goes idle; with
	// every request resolved the run then ends and the car goes offline.
	events := o.events("car_at_ferry_building")
	stopRemoved, idle, offline := events[len(events)-3], events[len(events)-2], events[len(events)-1]
	assertEquals(t, stopRemoved["action"], "stop_removed")
	assertEquals(t, stopRemoved["at"], at(360))
	assertEquals(t, idle["action"], "idle")
	assertEquals(t, idle["at"], at(360))
	assertEquals(t, offline["action"], "offline")
	assertEquals(t, offline["at"], at(360))
	assertEquals(t, o.runMetadata()["simulation_end_at"], at(360))
}

// Case 9: past the 300 s setting, within the 900 s override.
func TestPerRequestPickupLimitOverridesSetting(t *testing.T) {
	o := simulate(t, "short_pickup_timeout", "one_car_at_ferry_building", "single_trip_with_pickup_timeout_override")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["pickup_at"], at(697))
}
