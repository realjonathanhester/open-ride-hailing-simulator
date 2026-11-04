package tests

import (
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Component tests for [matching] max_candidate_cars (cases 24 to 28). The
// fake router gives every leg the same duration, so every car has an equal
// pickup ETA and only pruning decides which cars can win.

var (
	pruningPickup  = model.Location{Lat: 30.2672, Lng: -97.7431} // downtown Austin
	pruningDropoff = model.Location{Lat: 30.2849, Lng: -97.7341} // University of Texas
	nearPickup     = model.Location{Lat: 30.2680, Lng: -97.7431} // about 90 m from the pickup
	farFromPickup  = model.Location{Lat: 30.3000, Lng: -97.7431} // about 3.6 km from the pickup
	northOfPickup  = model.Location{Lat: 30.2700, Lng: -97.7431} // about 310 m north
	southOfPickup  = model.Location{Lat: 30.2644, Lng: -97.7431} // about 310 m south
)

// equalLegsRouter prices every leg at 100 s and records each car start it
// was asked to route from, plus every prefetch source.
type equalLegsRouter struct {
	routedFrom       map[model.Location]bool
	prefetchedSource map[model.Location]bool
	prefetchedTarget map[model.Location]bool
}

func newEqualLegsRouter() *equalLegsRouter {
	return &equalLegsRouter{map[model.Location]bool{}, map[model.Location]bool{}, map[model.Location]bool{}}
}

func (r *equalLegsRouter) Route(start model.Location, waypoints []model.Location, departAt int64) ([]model.Leg, error) {
	r.routedFrom[start] = true
	legs := make([]model.Leg, len(waypoints))
	for i := range legs {
		legs[i] = model.Leg{DistanceMeters: 1000, DurationSeconds: 100}
	}
	return legs, nil
}

func (r *equalLegsRouter) Prefetch(sources, destinations []model.Location, departAt int64) error {
	for _, s := range sources {
		r.prefetchedSource[s] = true
	}
	for _, d := range destinations {
		r.prefetchedTarget[d] = true
	}
	return nil
}

func idleCar(id string, at model.Location) model.SupplyView {
	return model.SupplyView{SupplyID: id, Location: at, State: model.StateIdle, NumAvailableSeats: 4}
}

// busyCar carries one rider to dropoffAt.
func busyCar(id string, at, dropoffAt model.Location) model.SupplyView {
	return model.SupplyView{
		SupplyID: id, Location: at, State: model.StateOnTrip, NumAvailableSeats: 4, SeatsInUse: 1,
		Plan: []model.Waypoint{{RequestID: "rider_of_" + id, Type: model.Dropoff, Location: dropoffAt, EtaAt: 500, NumRequestedSeats: 1}},
	}
}

func pruningRequest() model.RequestView {
	return model.RequestView{
		RequestID: "downtown_to_university", Origin: pruningPickup, Destination: pruningDropoff,
		NumRequestedSeats: 1, Status: model.StatusRequested,
	}
}

func match(limit int, pooling bool, router model.Router, supplies ...model.SupplyView) []model.PlanChange {
	settings := model.Settings{MaxCandidateCars: limit, PoolingEnabled: pooling, ReassignmentEnabled: true}
	return matching.NewGreedy().Match(model.MatchInput{
		Now: 0, Trigger: model.TriggerRequest, Settings: settings,
		Supplies: supplies, Requests: []model.RequestView{pruningRequest()}, Router: router,
	})
}

func stopsText(plan []model.Stop) string {
	var parts []string
	for _, s := range plan {
		parts = append(parts, s.RequestID+"."+string(s.Type))
	}
	return strings.Join(parts, ",")
}

// Case 24.
func TestPruningExcludesFarCarEvenWithEqualPickupTime(t *testing.T) {
	router := newEqualLegsRouter()
	changes := match(1, false, router, idleCar("car_a_far", farFromPickup), idleCar("car_b_near", nearPickup))
	assertEquals(t, itoa(int64(len(changes))), "1")
	assertEquals(t, changes[0].SupplyID, "car_b_near")
	assertEquals(t, boolText(router.routedFrom[farFromPickup]), "false")
	assertEquals(t, boolText(router.prefetchedSource[farFromPickup]), "false")
}

// Case 25.
func TestWithoutPruningEqualPickupTimesGoToLowestSupplyID(t *testing.T) {
	router := newEqualLegsRouter()
	changes := match(0, false, router, idleCar("car_a_far", farFromPickup), idleCar("car_b_near", nearPickup))
	assertEquals(t, itoa(int64(len(changes))), "1")
	assertEquals(t, changes[0].SupplyID, "car_a_far")
	assertEquals(t, boolText(router.routedFrom[farFromPickup]), "true")
}

// Case 26.
func TestPruningTieOnDistanceKeepsLowestSupplyID(t *testing.T) {
	router := newEqualLegsRouter()
	changes := match(1, false, router, idleCar("car_z_north", northOfPickup), idleCar("car_y_south", southOfPickup))
	assertEquals(t, itoa(int64(len(changes))), "1")
	assertEquals(t, changes[0].SupplyID, "car_y_south")
	assertEquals(t, boolText(router.routedFrom[northOfPickup]), "false")
}

// Case 27: the limit applies separately to open cars and to busy cars.
func TestPruningLimitsPoolingCandidatesSeparately(t *testing.T) {
	router := newEqualLegsRouter()
	changes := match(1, true, router,
		busyCar("car_a_busy_far", farFromPickup, pruningDropoff),
		busyCar("car_b_busy_near", nearPickup, pruningDropoff),
		idleCar("car_c_open_far", southOfPickup),
	)
	assertEquals(t, itoa(int64(len(changes))), "1")
	assertEquals(t, changes[0].SupplyID, "car_b_busy_near")
	assertEquals(t, stopsText(changes[0].Plan), "downtown_to_university.pickup,downtown_to_university.dropoff,rider_of_car_b_busy_near.dropoff")
	assertEquals(t, boolText(router.routedFrom[farFromPickup]), "false")
	assertEquals(t, boolText(router.routedFrom[southOfPickup]), "true")
	assertEquals(t, boolText(router.prefetchedTarget[pruningDropoff]), "true")
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Case 28: max_candidate_cars = 0 is the default and changes nothing, not
// even the settings hash.
func TestZeroCandidateLimitIsTheDefault(t *testing.T) {
	base := simulate(t, "base_default", "ferry_building_car_then_union_square_car_online_late", "single_trip")
	explicit := simulate(t, "no_candidate_limit", "ferry_building_car_then_union_square_car_online_late", "single_trip")
	assertFile(t, runner.TripsFile, explicit.files[runner.TripsFile], base.files[runner.TripsFile])
	assertFile(t, runner.SupplyEventsFile, explicit.files[runner.SupplyEventsFile], base.files[runner.SupplyEventsFile])
	assertFile(t, runner.SupplySummaryFile, explicit.files[runner.SupplySummaryFile], base.files[runner.SupplySummaryFile])
	assertFile(t, runner.RunSummaryFile, explicit.runSummaryWithoutTimings(), base.runSummaryWithoutTimings())
	assertFile(t, runner.RunMetadataFile, explicit.files[runner.RunMetadataFile], base.files[runner.RunMetadataFile])
}
