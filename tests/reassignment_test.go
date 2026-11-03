package tests

import "testing"

// Case 12: at T+120 the Ferry Building car is 577 s away and the Union Square car 491 s.
func TestCloserCarTakesOverWhenReassignmentEnabled(t *testing.T) {
	o := simulate(t, "base_default", "ferry_building_car_then_union_square_car_online_late", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["supply_id"], "car_at_union_square")
	assertEquals(t, trip["reassign_count"], "1")
	assertEquals(t, trip["first_assigned_at"], at(60))
	assertEquals(t, trip["assigned_at"], at(120))
	assertEquals(t, trip["eta_at_assignment_seconds"], "491")
}

// Case 13.
func TestAssignedCarKeepsRequestWhenReassignmentDisabled(t *testing.T) {
	o := simulate(t, "reassignment_disabled", "ferry_building_car_then_union_square_car_online_late", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["supply_id"], "car_at_ferry_building")
	assertEquals(t, trip["reassign_count"], "0")
}
