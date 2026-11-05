package tests

import (
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Case 32: two-car successful handoff with metrics (onward after dropoff).
func TestTwoCarSuccessfulHandoff(t *testing.T) {
	o := simulate(t, "handoffs_enabled", "kezar_and_union_square_cars", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["supply_id"], "car_at_union_square")
	assertEquals(t, trip["handoff_count"], "1")
	assertEquals(t, trip["pickup_at"], at(60))
	assertEquals(t, trip["wait_seconds"], "0")
	assertEquals(t, trip["trip_distance_meters"], "6516")

	legs := o.rows(runner.TripLegsFile)
	if len(legs) != 2 {
		t.Fatalf("trip_legs rows = %d, want 2", len(legs))
	}
	assertEquals(t, legs[0]["supply_id"], "car_at_kezar_stadium")
	assertEquals(t, legs[0]["distance_meters"], "4908")
	assertEquals(t, legs[1]["supply_id"], "car_at_union_square")
	assertEquals(t, legs[1]["distance_meters"], "1608")
	assertEquals(t, o.eventsWithAction("car_at_kezar_stadium", "dropoff")[0]["at"], at(551))
}

// Case 33: wrong onward location rejects the onward plan.
func TestWrongOnwardLocationRejectsBatch(t *testing.T) {
	o := simulate(t, "handoffs_invalid_onward", "kezar_and_union_square_cars", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "canceled")
	assertEquals(t, trip["cancel_reason"], "max_handoff_wait")
	if len(o.eventsWithAction("car_at_kezar_stadium", "plan_updated")) == 0 {
		t.Fatalf("first-leg handoff should have been accepted")
	}
	for _, event := range o.events("car_at_union_square") {
		if event["action"] == "plan_updated" {
			t.Fatalf("union car should not accept a wrong onward location")
		}
	}
}

// Case 34: duplicate onward pickups reject the batch.
func TestDuplicateOnwardPickupRejectsBatch(t *testing.T) {
	o := simulate(t, "handoffs_duplicate_onward", "kezar_ferry_and_union_rematch", "single_trip")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["cancel_reason"], "max_handoff_wait")
	for _, id := range []string{"car_at_union_square", "car_at_ferry_building"} {
		if len(o.eventsWithAction(id, "plan_updated")) != 0 {
			t.Fatalf("%s got plan_updated; duplicate onward should reject the batch", id)
		}
	}
}

// Case 35: linked batch with one invalid plan rolls back every change.
func TestLinkedHandoffBatchAllOrNothingRejection(t *testing.T) {
	o := simulate(t, "handoffs_reject_batch", "kezar_and_union_square_cars", "single_trip")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["status"], "canceled")
	for _, id := range []string{"car_at_kezar_stadium", "car_at_union_square"} {
		if len(o.eventsWithAction(id, "plan_updated")) != 0 {
			t.Fatalf("%s kept a plan after linked batch rejection", id)
		}
		if len(o.eventsWithAction(id, "plan_rejected")) == 0 {
			t.Fatalf("%s missing plan_rejected", id)
		}
	}
}

// Case 36: onward car goes offline; rider waits then times out.
func TestOnwardCarOfflineThenHandoffTimeout(t *testing.T) {
	o := simulate(t, "handoffs_offline_timeout", "kezar_and_ferry_for_handoff", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "canceled")
	assertEquals(t, trip["cancel_reason"], "max_handoff_wait")
	assertEquals(t, trip["handoff_count"], "1")
	if len(o.eventsWithAction("car_at_ferry_building", "stop_removed")) == 0 {
		t.Fatalf("expected stop_removed when onward car went offline")
	}
}

// Case 37: after onward offline, a later matcher call assigns a replacement.
func TestOnwardCarOfflineThenRematch(t *testing.T) {
	o := simulate(t, "handoffs_offline_rematch", "kezar_ferry_and_union_rematch", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["supply_id"], "car_at_union_square")
	assertEquals(t, trip["handoff_count"], "1")
	if len(o.eventsWithAction("car_at_ferry_building", "stop_removed")) == 0 {
		t.Fatalf("expected stop_removed on offline onward car")
	}
}

// Case 38: handoff deadline fires while onward leg is assigned / en route.
func TestHandoffDeadlineDuringOnwardAssignment(t *testing.T) {
	o := simulate(t, "handoffs_deadline_during_onward", "kezar_and_ferry_for_handoff", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "canceled")
	assertEquals(t, trip["cancel_reason"], "max_handoff_wait")
	if len(o.eventsWithAction("car_at_ferry_building", "stop_removed")) == 0 {
		t.Fatalf("expected stop_removed on timeout cleanup")
	}
}

// Case 39: non-handoff runs omit new columns and trip_legs.
func TestHandoffsDisabledOmitNewOutputs(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium", "single_trip")
	if strings.Contains(o.files[runner.TripsFile], "handoff_count") {
		t.Fatalf("handoff_count present without handoffs enabled")
	}
	if _, ok := o.files[runner.TripLegsFile]; ok {
		t.Fatalf("trip_legs.csv written without handoffs enabled")
	}
}

// Case 40: seats freed at handoff dropoff so a one-seat car can take another rider.
func TestCapacityFreedAcrossHandoff(t *testing.T) {
	o := simulate(t, "handoffs_pooling", "kezar_and_union_square_cars", "simple_pool")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["status"], "completed")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["handoff_count"], "1")
	assertEquals(t, o.trip("painted_ladies_to_union_square")["status"], "completed")
	assertEquals(t, o.trip("painted_ladies_to_union_square")["supply_id"], "car_at_kezar_stadium")
}

// Case 41: matcher drops an onward pickup; assignment fields clear and stop_removed logs.
func TestMatcherRemovalOfOnwardPickupUnassignsVisibly(t *testing.T) {
	o := simulate(t, "handoffs_matcher_drop", "kezar_and_ferry_for_handoff", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	// After drop, rider is waiting again then times out or stays unserved until wait.
	assertEquals(t, trip["cancel_reason"], "max_handoff_wait")
	removed := o.eventsWithAction("car_at_ferry_building", "stop_removed")
	if len(removed) == 0 {
		t.Fatalf("expected stop_removed when matcher dropped the onward pickup")
	}
	assertEquals(t, trip["assigned_at"], "")
	assertEquals(t, trip["supply_id"], "")
}

// Case 42: greedy preserves an existing handoff dropoff location across calls.
func TestGreedyPreservesExistingHandoffPlan(t *testing.T) {
	o := simulate(t, "handoffs_then_greedy", "kezar_and_union_square_cars", "single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["status"], "completed")
	assertEquals(t, trip["handoff_count"], "1")
	// First-leg dropoff must remain at Union Square, not be rewritten to Coit Tower.
	// The handoff dropoff at Union Square must appear even after later greedy calls.
	foundHandoff := false
	for _, dropoff := range o.eventsWithAction("car_at_kezar_stadium", "dropoff") {
		if dropoff["lat"] == "37.787994" && dropoff["lng"] == "-122.407437" {
			foundHandoff = true
		}
	}
	if !foundHandoff {
		t.Fatalf("kezar never dropped at the handoff location; greedy may have stripped it")
	}
}

// Case 43: a third-party trip can use the eventual onward car before the handoff.
func TestThirdPartyAssignmentBeforeHandoff(t *testing.T) {
	o := simulate(t, "handoffs_third_party", "kezar_and_union_square_cars", "handoff_and_painted_ladies")
	assertEquals(t, o.trip("painted_ladies_to_union_square")["status"], "completed")
	assertEquals(t, o.trip("painted_ladies_to_union_square")["supply_id"], "car_at_union_square")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["status"], "completed")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["handoff_count"], "1")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["supply_id"], "car_at_union_square")
}

// Case 44: canceling an onward rider on a shared car keeps an aboard rider's dropoff.
func TestCancelOnwardPreservesAboardRiderDropoff(t *testing.T) {
	o := simulate(t, "handoffs_pooled_cancel", "kezar_and_ferry_for_handoff", "ferry_and_kezar_handoff")
	assertEquals(t, o.trip("kezar_stadium_to_coit_tower")["cancel_reason"], "max_handoff_wait")
	other := o.trip("ferry_building_to_coit_tower")
	assertEquals(t, other["status"], "completed")
	assertEquals(t, other["supply_id"], "car_at_ferry_building")
	// Ferry must not go negative seats or lose the aboard rider mid-cancel.
	for _, event := range o.events("car_at_ferry_building") {
		if event["seats_in_use"] != "" && number(t, event["seats_in_use"]) < 0 {
			t.Fatalf("negative seats_in_use after cancel: %v", event)
		}
	}
}


// Case 45: after a handoff dropoff, the original max_request_to_pickup deadline
// must not cancel the rider; only max_handoff_wait_seconds governs.
func TestOriginalPickupDeadlineDoesNotCancelAfterHandoff(t *testing.T) {
	o := simulate(t, "handoffs_pickup_deadline_after_handoff", "one_car_at_kezar_stadium", "late_handoff_single_trip")
	trip := o.trip("kezar_stadium_to_coit_tower")
	assertEquals(t, trip["handoff_count"], "1")
	// Must not have been canceled by the original pickup deadline at T+1200.
	if trip["canceled_at"] == at(1200) && trip["cancel_reason"] == "max_request_to_pickup" {
		t.Fatalf("original pickup deadline canceled after handoff")
	}
	assertEquals(t, trip["status"], "canceled")
	assertEquals(t, trip["cancel_reason"], "max_handoff_wait")
	// Handoff at T+1190; handoff wait 120s → cancel at T+1310.
	assertEquals(t, trip["canceled_at"], at(1310))
}
