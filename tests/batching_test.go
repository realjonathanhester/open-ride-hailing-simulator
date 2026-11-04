package tests

import (
	"strconv"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Case 14: trips match case 2, which runs without batch ticks.
func TestBatchTicksWithoutChangesAreNoOps(t *testing.T) {
	o := simulate(t, "batch_every_60_seconds", "one_car_at_union_square", "single_trip")
	planUpdates := o.eventsWithAction("car_at_union_square", "plan_updated")
	assertEquals(t, strconv.Itoa(len(planUpdates)), "1")

	withoutBatching := simulate(t, "base_default", "one_car_at_union_square", "single_trip")
	assertFile(t, "trips", o.files[runner.TripsFile], withoutBatching.files[runner.TripsFile])
}
