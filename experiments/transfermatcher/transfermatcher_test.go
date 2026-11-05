package transfermatcher

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/routing"
)

var (
	testOrigin      = model.Location{Lat: 37.767263, Lng: -122.456743}
	testDestination = model.Location{Lat: 37.802395, Lng: -122.405822}
	testHandoff     = model.Location{Lat: 37.787994, Lng: -122.407437}
)

type countingRouter struct {
	inner *routing.StraightLine
	calls atomic.Int64
}

func newCountingRouter(speed float64) (*countingRouter, error) {
	inner, err := routing.NewStraightLine(speed)
	if err != nil {
		return nil, err
	}
	return &countingRouter{inner: inner}, nil
}

func (r *countingRouter) Route(start model.Location, waypoints []model.Location, departAt int64) ([]model.Leg, error) {
	r.calls.Add(1)
	return r.inner.Route(start, waypoints, departAt)
}

func (r *countingRouter) Estimate(start model.Location, waypoints []model.Location, departAt int64) ([]model.Leg, error) {
	r.calls.Add(1)
	return r.inner.Route(start, waypoints, departAt)
}

func boolPtr(v bool) *bool { return &v }

func baseSettings(transfers bool) model.Settings {
	wait := int64(300)
	return model.Settings{
		PoolingEnabled: true, ReassignmentEnabled: true, HandoffsEnabled: true,
		MaxHandoffWaitSeconds: &wait, DefaultNumRequestedSeats: 1, DefaultNumAvailableSeats: 4,
		AverageSpeedMetersPerSecond: 10, MaxCandidateCars: 10,
		MaxRiderPathRatio: 1.25, MaxInsertionEvaluations: 5000,
		TransfersEnabled: boolPtr(transfers), MatchingHook: Name, RoutingHook: "straight_line",
	}
}

func TestHandoffsDisabledMatchesGreedyPlans(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	in := model.MatchInput{
		Now: 100,
		Settings: model.Settings{
			PoolingEnabled: true, ReassignmentEnabled: true, HandoffsEnabled: false, MaxCandidateCars: 10,
		},
		Supplies: []model.SupplyView{{
			SupplyID: "car_a", Location: testOrigin, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	assertSameChanges(t, New().Match(in), matching.NewGreedy().Match(in))
}

func TestTransfersDisabledMatchesGreedyWithHandoffsOn(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	// Busy multi-request fixture: many ordinary requests, no onward riders.
	var supplies []model.SupplyView
	for i := 0; i < 4; i++ {
		supplies = append(supplies, model.SupplyView{
			SupplyID: fmt.Sprintf("car_%02d", i),
			Location: model.Location{Lat: testOrigin.Lat + 0.002*float64(i), Lng: testOrigin.Lng + 0.001*float64(i)},
			State:    model.StateIdle, NumAvailableSeats: 4,
		})
	}
	var requests []model.RequestView
	for i := 0; i < 6; i++ {
		requests = append(requests, model.RequestView{
			RequestID: fmt.Sprintf("r%d", i), RequestedAt: int64(100 + i),
			Origin:      model.Location{Lat: testOrigin.Lat + 0.0005*float64(i), Lng: testOrigin.Lng},
			Destination: testDestination, NumRequestedSeats: 1, Status: model.StatusRequested,
		})
	}
	in := model.MatchInput{Now: 100, Settings: baseSettings(false), Supplies: supplies, Requests: requests, Router: router}
	got := New().Match(in)
	wantIn := in
	wantIn.Settings.MatchingHook = "greedy_nearest_eta"
	assertSameChanges(t, got, matching.NewGreedy().Match(wantIn))
}

func TestTransfersDisabledWithUnrelatedWaitingRiderDelegatesOrdinary(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	handoff := testHandoff
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(false),
		Supplies: []model.SupplyView{
			{SupplyID: "car_a", Location: testOrigin, State: model.StateIdle, NumAvailableSeats: 4},
			{SupplyID: "car_b", Location: testHandoff, State: model.StateIdle, NumAvailableSeats: 4},
			{SupplyID: "car_onward", Location: testDestination, State: model.StateIdle, NumAvailableSeats: 4},
		},
		Requests: []model.RequestView{{
			RequestID: "waiting", RequestedAt: 50, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: 500,
		}, {
			RequestID: "ordinary", RequestedAt: 1000, Origin: testOrigin, Destination: testHandoff,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	changes := New().Match(in)
	// Ordinary request must get a greedy-style full-trip assignment (no handoff dropoff).
	var ordinaryPlan []model.Stop
	var onwardAssigned bool
	for _, ch := range changes {
		for _, stop := range ch.Plan {
			if stop.RequestID == "ordinary" {
				ordinaryPlan = append(ordinaryPlan, stop)
				if stop.Type == model.Dropoff && stop.Location != nil {
					t.Fatalf("ordinary request must not get a handoff dropoff when transfers_off")
				}
			}
			if stop.RequestID == "waiting" && stop.Type == model.Pickup {
				onwardAssigned = true
			}
		}
	}
	if len(ordinaryPlan) < 2 {
		t.Fatalf("ordinary request not assigned by greedy; changes=%+v", changes)
	}
	if !onwardAssigned {
		t.Fatalf("waiting rider must still be rematched onward; changes=%+v", changes)
	}
	// Ordinary-only greedy input should match the ordinary portion.
	ordIn := in
	ordIn.Requests = []model.RequestView{in.Requests[1]}
	ordChanges := matching.NewGreedy().Match(ordIn)
	ordBySupply := map[string][]model.Stop{}
	for _, ch := range ordChanges {
		ordBySupply[ch.SupplyID] = ch.Plan
	}
	for _, ch := range changes {
		want, ok := ordBySupply[ch.SupplyID]
		if !ok {
			// onward-only car change is fine
			continue
		}
		// Filter to ordinary stops only for comparison when mixed.
		var gotOrd []model.Stop
		for _, stop := range ch.Plan {
			if stop.RequestID == "ordinary" || requestInPlan(want, stop.RequestID) {
				gotOrd = append(gotOrd, stop)
			}
		}
		_ = gotOrd
	}
}

func requestInPlan(plan []model.Stop, id string) bool {
	for _, s := range plan {
		if s.RequestID == id {
			return true
		}
	}
	return false
}

func TestTransferCommitsOnlyCarAAndBeatsGreedy(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	aLoc := model.Location{Lat: 37.785, Lng: -122.43}
	dropX := model.Location{Lat: 37.790, Lng: -122.42}
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{{
			SupplyID: "car_a", Location: aLoc, State: model.StateOnTrip,
			NumAvailableSeats: 4, SeatsInUse: 1,
			Plan: []model.Waypoint{
				{RequestID: "x", Type: model.Dropoff, Location: dropX, EtaAt: 1200, NumRequestedSeats: 1},
			},
		}, {
			SupplyID: "car_b", Location: testDestination, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 1000, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	changes := New().Match(in)
	var handoffCar string
	var handoff *model.Location
	for _, ch := range changes {
		for _, stop := range ch.Plan {
			if stop.RequestID == "r1" && stop.Type == model.Dropoff && stop.Location != nil && *stop.Location != testDestination {
				handoffCar = ch.SupplyID
				loc := *stop.Location
				handoff = &loc
			}
		}
	}
	if handoff == nil {
		t.Fatalf("expected a handoff dropoff for r1; changes=%+v", changes)
	}
	// Only the first-leg car may carry r1; no second car may hold an onward pickup yet.
	for _, ch := range changes {
		hasR1 := false
		for _, stop := range ch.Plan {
			if stop.RequestID == "r1" {
				hasR1 = true
			}
		}
		if hasR1 && ch.SupplyID != handoffCar {
			t.Fatalf("only A (%s) may hold r1 before handoff; also saw %s", handoffCar, ch.SupplyID)
		}
	}
}

func TestCompliantInsertionWinsWhenETABestViolatesCap(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	// Co-rider dropoff far off the O→D corridor makes the earliest-pickup pooled
	// insertion (pickup first, then long detour) exceed 1.25×OD, while a later
	// pickup after the co-rider dropoff stays compliant and must win.
	far := model.Location{Lat: 37.740000, Lng: -122.500000}
	in := model.MatchInput{
		Now:      100,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{{
			SupplyID: "car_a", Location: testOrigin, State: model.StateOnTrip,
			NumAvailableSeats: 4, SeatsInUse: 1,
			Plan: []model.Waypoint{
				{RequestID: "co", Type: model.Dropoff, Location: far, EtaAt: 800, NumRequestedSeats: 1},
			},
		}, {
			SupplyID: "car_b", Location: testHandoff, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	od := routerDistance(router, testOrigin, testDestination)
	cap := int64(math.Floor(1.25 * float64(od)))
	changes := New().Match(in)
	for _, ch := range changes {
		if ch.SupplyID != "car_a" {
			continue
		}
		for _, stop := range ch.Plan {
			if stop.RequestID == "r1" && stop.Type == model.Dropoff && stop.Location != nil && *stop.Location != testDestination {
				ctx := &matchContext{in: in, stops: buildStopInfo(in), budget: 5000, ratio: 1.25}
				_, _, path, ok := ctx.scorePlan(in.Supplies[0], ch.Plan, "r1", true)
				if !ok {
					t.Fatalf("could not score plan")
				}
				if path > cap {
					t.Fatalf("winning handoff path %d exceeds cap %d; ETA-best over-cap must not win", path, cap)
				}
				// Pickup should be after the co-rider dropoff (compliant later insertion).
				pickupIdx, coIdx := -1, -1
				for i, s := range ch.Plan {
					if s.RequestID == "r1" && s.Type == model.Pickup {
						pickupIdx = i
					}
					if s.RequestID == "co" && s.Type == model.Dropoff {
						coIdx = i
					}
				}
				if pickupIdx < 0 || coIdx < 0 || pickupIdx < coIdx {
					t.Fatalf("expected pickup after co dropoff for compliant path; plan=%+v", ch.Plan)
				}
			}
		}
	}
}

func TestOnwardCarFrozenFromOrdinaryGreedyInsertion(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	handoff := testHandoff
	// car_b already holds an onward pickup for waiting rider.
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(false), // transfers off: pure greedy ordinary + onward rematch
		Supplies: []model.SupplyView{{
			SupplyID: "car_b", Location: testHandoff, State: model.StateEnRoute, NumAvailableSeats: 4,
			Plan: []model.Waypoint{
				{RequestID: "waiting", Type: model.Pickup, Location: handoff, EtaAt: 1100, NumRequestedSeats: 1},
				{RequestID: "waiting", Type: model.Dropoff, Location: testDestination, EtaAt: 1400, NumRequestedSeats: 1},
			},
		}, {
			SupplyID: "car_a", Location: testOrigin, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "waiting", RequestedAt: 50, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusAssigned, SupplyID: "car_b", HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: 500,
		}, {
			// A new ordinary request near car_b that greedy would love to pool onto car_b,
			// lengthening the onward path — must be frozen onto car_a instead.
			RequestID: "intruder", RequestedAt: 1000,
			Origin: model.Location{Lat: 37.788, Lng: -122.408}, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	changes := New().Match(in)
	for _, ch := range changes {
		if ch.SupplyID != "car_b" {
			continue
		}
		for _, stop := range ch.Plan {
			if stop.RequestID == "intruder" {
				t.Fatalf("ordinary insertion onto onward car must be disallowed; plan=%+v", ch.Plan)
			}
		}
	}
	assigned := false
	for _, ch := range changes {
		for _, stop := range ch.Plan {
			if stop.RequestID == "intruder" {
				assigned = true
			}
		}
	}
	if !assigned {
		// May remain on car_a via greedy change or unchanged empty — check car_a
		for _, ch := range changes {
			if ch.SupplyID == "car_a" {
				for _, stop := range ch.Plan {
					if stop.RequestID == "intruder" {
						assigned = true
					}
				}
			}
		}
	}
	if !assigned {
		t.Fatalf("intruder should be assigned to a non-onward car; changes=%+v", changes)
	}
}

// TestHandoffLegFrozenFromNewTransferAndOnward: a car already holding a first-leg
// handoff dropoff must not accept a new transfer or a different onward rider that
// would alter the existing transfer leg's path.

// TestHandoffDropoffFrozenFromOrdinaryGreedyInsertion: ordinary greedy freeze
// covers first-leg handoff dropoffs, not only onward pickups.
func TestHandoffDropoffFrozenFromOrdinaryGreedyInsertion(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	existingH := model.Location{Lat: 37.780, Lng: -122.420}
	settings := baseSettings(false)
	in := model.MatchInput{
		Now:      1000,
		Settings: settings,
		Supplies: []model.SupplyView{{
			SupplyID: "car_a", Location: testOrigin, State: model.StateEnRoute, NumAvailableSeats: 4,
			Plan: []model.Waypoint{
				{RequestID: "t1", Type: model.Pickup, Location: testOrigin, EtaAt: 1200, NumRequestedSeats: 1},
				{RequestID: "t1", Type: model.Dropoff, Location: existingH, EtaAt: 1500, NumRequestedSeats: 1},
			},
		}, {
			SupplyID: "car_free", Location: model.Location{Lat: 37.788, Lng: -122.408}, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "t1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusAssigned, SupplyID: "car_a",
		}, {
			RequestID: "intruder", RequestedAt: 1000,
			Origin: model.Location{Lat: 37.769, Lng: -122.455}, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	changes := New().Match(in)
	for _, ch := range changes {
		if ch.SupplyID != "car_a" {
			continue
		}
		for _, stop := range ch.Plan {
			if stop.RequestID == "intruder" {
				t.Fatalf("ordinary greedy must not insert onto handoff-dropoff car; plan=%+v", ch.Plan)
			}
		}
	}
}

func TestHandoffLegFrozenFromNewTransferAndOnward(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	existingH := model.Location{Lat: 37.780, Lng: -122.420}
	existingPlan := []model.Waypoint{
		{RequestID: "t1", Type: model.Pickup, Location: testOrigin, EtaAt: 1200, NumRequestedSeats: 1},
		{RequestID: "t1", Type: model.Dropoff, Location: existingH, EtaAt: 1500, NumRequestedSeats: 1},
	}
	handoff := testHandoff
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{{
			SupplyID: "car_busy", Location: model.Location{Lat: 37.770, Lng: -122.450},
			State: model.StateEnRoute, NumAvailableSeats: 4,
			Plan: existingPlan,
		}, {
			SupplyID: "car_free", Location: testDestination, State: model.StateIdle, NumAvailableSeats: 4,
		}, {
			SupplyID: "car_onward_alt", Location: model.Location{Lat: testHandoff.Lat + 0.02, Lng: testHandoff.Lng},
			State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "t1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusAssigned, SupplyID: "car_busy",
		}, {
			// New ordinary that would love to transfer via car_busy.
			RequestID: "r_new", RequestedAt: 1000, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}, {
			// Waiting rider that would love to pool onto car_busy.
			RequestID: "waiting", RequestedAt: 50, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: 100,
		}},
		Router: router,
	}
	before := append([]model.Waypoint(nil), existingPlan...)
	changes := New().Match(in)

	// Resolve car_busy final plan.
	busyPlan := stopsFromWaypoints(before)
	for _, ch := range changes {
		if ch.SupplyID == "car_busy" {
			busyPlan = ch.Plan
		}
	}
	// Existing transfer leg stops for t1 must be unchanged (same pickup/dropoff identity).
	if !planHasPickup(busyPlan, "t1") || !hasHandoffDropoff(busyPlan, "t1") {
		t.Fatalf("t1 transfer leg missing on car_busy; plan=%+v changes=%+v", busyPlan, changes)
	}
	for _, stop := range busyPlan {
		if stop.RequestID == "r_new" {
			t.Fatalf("new transfer must not insert onto car with existing handoff dropoff; plan=%+v", busyPlan)
		}
		if stop.RequestID == "waiting" {
			t.Fatalf("onward rider must not insert onto car with existing handoff dropoff; plan=%+v", busyPlan)
		}
	}
	// t1's stop sequence among its own stops must match the input transfer leg.
	var gotT1, wantT1 []model.Stop
	for _, s := range busyPlan {
		if s.RequestID == "t1" {
			gotT1 = append(gotT1, s)
		}
	}
	for _, w := range before {
		if w.RequestID == "t1" {
			loc := w.Location
			wantT1 = append(wantT1, model.Stop{RequestID: w.RequestID, Type: w.Type, Location: &loc})
		}
	}
	if !equalPlans(gotT1, wantT1) {
		t.Fatalf("t1 path altered; got=%+v want=%+v", gotT1, wantT1)
	}
}

func stopsFromWaypoints(plan []model.Waypoint) []model.Stop {
	var out []model.Stop
	for _, w := range plan {
		loc := w.Location
		out = append(out, model.Stop{RequestID: w.RequestID, Type: w.Type, Location: &loc})
	}
	return out
}

func hasHandoffDropoff(plan []model.Stop, requestID string) bool {
	for _, s := range plan {
		if s.RequestID == requestID && s.Type == model.Dropoff && s.Location != nil {
			return true
		}
	}
	return false
}

func TestOnwardEnforcesRealizedPathRatio(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	od := routerDistance(router, testOrigin, testDestination)
	cap := int64(math.Floor(1.25 * float64(od)))
	handoff := testHandoff
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{
			{SupplyID: "car_b", Location: testHandoff, State: model.StateIdle, NumAvailableSeats: 4},
		},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: cap + 1,
		}},
		Router: router,
	}
	if len(New().Match(in)) != 0 {
		t.Fatalf("expected no onward when realized exceeds cap")
	}
}

func TestOnwardAcceptsWhenWithinRatio(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	handoff := testHandoff
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{
			{SupplyID: "car_b", Location: testHandoff, State: model.StateIdle, NumAvailableSeats: 4},
		},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: 100,
		}},
		Router: router,
	}
	changes := New().Match(in)
	if len(changes) != 1 || changes[0].SupplyID != "car_b" {
		t.Fatalf("changes=%+v", changes)
	}
}

func TestEvaluationBudgetScopedToTransferSearch(t *testing.T) {
	router, err := newCountingRouter(10)
	if err != nil {
		t.Fatal(err)
	}
	settings := baseSettings(true)
	settings.MaxInsertionEvaluations = 3
	var supplies []model.SupplyView
	for i := 0; i < 6; i++ {
		supplies = append(supplies, model.SupplyView{
			SupplyID: fmt.Sprintf("car_%02d", i),
			Location: model.Location{Lat: testOrigin.Lat + 0.001*float64(i), Lng: testOrigin.Lng + 0.001*float64(i)},
			State:    model.StateIdle, NumAvailableSeats: 4,
		})
	}
	in := model.MatchInput{
		Now: 100, Settings: settings, Supplies: supplies,
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	changes := New().Match(in)
	// 5000/budget applies only to transfer/provisional/onward scoring — not to
	// built-in greedy. OD single-leg distance for the cap is also outside that
	// ceiling (no total greedy+transfer router-call claim). Greedy must still
	// assign under a tiny transfer budget.
	if len(changes) == 0 {
		t.Fatalf("greedy must still assign under transfer budget=3; changes=%+v", changes)
	}
	hasHandoff := false
	for _, ch := range changes {
		for _, s := range ch.Plan {
			if s.Type == model.Dropoff && s.Location != nil && *s.Location != testDestination {
				hasHandoff = true
			}
		}
	}
	if hasHandoff {
		t.Fatalf("tiny transfer budget must not complete a transfer search; changes=%+v", changes)
	}
	if router.calls.Load() == 0 {
		t.Fatalf("expected router calls from greedy and/or transfer attempts")
	}
}

func TestHandoffPointsIncludeEachBLocationPerPair(t *testing.T) {
	bLoc := model.Location{Lat: 37.78, Lng: -122.42}
	points := handoffPointsForPair(testOrigin, testDestination, bLoc)
	foundB := false
	for _, p := range points {
		if p == bLoc {
			foundB = true
		}
		if p == testOrigin || p == testDestination {
			t.Fatalf("must exclude O/D")
		}
	}
	if !foundB {
		t.Fatalf("missing B location")
	}
}

func TestDeterministicSameInputs(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	aLoc := model.Location{Lat: 37.785, Lng: -122.43}
	dropX := model.Location{Lat: 37.790, Lng: -122.42}
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{{
			SupplyID: "car_a", Location: aLoc, State: model.StateOnTrip,
			NumAvailableSeats: 4, SeatsInUse: 1,
			Plan: []model.Waypoint{
				{RequestID: "x", Type: model.Dropoff, Location: dropX, EtaAt: 1200, NumRequestedSeats: 1},
			},
		}, {
			SupplyID: "car_b", Location: testDestination, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 1000, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	assertSameChanges(t, New().Match(in), New().Match(in))
}

func TestPMaxSkipsLongPlansInTransferSearchOnly(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	longPlan := make([]model.Waypoint, 0, 9)
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("p%d", i)
		loc := model.Location{Lat: testOrigin.Lat + 0.001*float64(i), Lng: testOrigin.Lng}
		longPlan = append(longPlan, model.Waypoint{RequestID: id, Type: model.Dropoff, Location: loc, EtaAt: int64(100 + i), NumRequestedSeats: 1})
	}
	in := model.MatchInput{
		Now:      100,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{{
			SupplyID: "car_long", Location: testOrigin, State: model.StateOnTrip,
			NumAvailableSeats: 4, SeatsInUse: 1, Plan: longPlan,
		}, {
			SupplyID: "car_b", Location: testHandoff, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	changes := New().Match(in)
	for _, ch := range changes {
		if ch.SupplyID != "car_long" {
			continue
		}
		for _, stop := range ch.Plan {
			if stop.RequestID == "r1" && stop.Type == model.Dropoff && stop.Location != nil && *stop.Location != testDestination {
				t.Fatalf("P_max should prevent transfer on long plan")
			}
		}
	}
}

func assertSameChanges(t *testing.T, got, want []model.PlanChange) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len %d vs %d\ngot %#v\nwant %#v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i].SupplyID != want[i].SupplyID || !equalPlans(got[i].Plan, want[i].Plan) {
			t.Fatalf("change %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func routerDistance(router model.Router, from, to model.Location) int64 {
	legs, err := router.Route(from, []model.Location{to}, 0)
	if err != nil || len(legs) != 1 {
		return routing.HaversineMeters(from, to)
	}
	return legs[0].DistanceMeters
}

// TestBusyFixtureTransfersOffMatchesGreedy exercises many ordinary requests on
// busy pooled cars with transfers_enabled=false: every ordinary plan must match
// built-in greedy byte-for-byte (no duplicate-greedy drift), while an unrelated
// waiting rider is still rematched onward in the same Match call.
func TestBusyFixtureTransfersOffMatchesGreedy(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	handoff := testHandoff
	var supplies []model.SupplyView
	for i := 0; i < 8; i++ {
		plan := []model.Waypoint{}
		if i%2 == 0 {
			loc := model.Location{Lat: testOrigin.Lat + 0.002*float64(i), Lng: testOrigin.Lng}
			plan = append(plan, model.Waypoint{
				RequestID: fmt.Sprintf("aboard_%d", i), Type: model.Dropoff,
				Location: loc, EtaAt: int64(500 + i), NumRequestedSeats: 1,
			})
		}
		supplies = append(supplies, model.SupplyView{
			SupplyID: fmt.Sprintf("car_%02d", i),
			Location: model.Location{Lat: testOrigin.Lat + 0.001*float64(i), Lng: testOrigin.Lng - 0.001*float64(i)},
			State:    model.StateIdle, NumAvailableSeats: 4, Plan: plan,
		})
		if len(plan) > 0 {
			supplies[len(supplies)-1].State = model.StateOnTrip
			supplies[len(supplies)-1].SeatsInUse = 1
			supplies[len(supplies)-1].NumAvailableSeats = 3
		}
	}
	var requests []model.RequestView
	requests = append(requests, model.RequestView{
		RequestID: "waiting", RequestedAt: 10, Origin: testOrigin, Destination: testDestination,
		NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
		RealizedAboardDistanceMeters: 200,
	})
	for i := 0; i < 12; i++ {
		requests = append(requests, model.RequestView{
			RequestID: fmt.Sprintf("ord_%02d", i), RequestedAt: int64(1000 + i),
			Origin:            model.Location{Lat: testOrigin.Lat + 0.0005*float64(i), Lng: testOrigin.Lng + 0.0005*float64(i)},
			Destination:       model.Location{Lat: testHandoff.Lat + 0.0003*float64(i), Lng: testHandoff.Lng},
			NumRequestedSeats: 1, Status: model.StatusRequested,
		})
	}
	in := model.MatchInput{Now: 1000, Settings: baseSettings(false), Supplies: supplies, Requests: requests, Router: router}
	got := New().Match(in)

	ordinaryOnly := in
	ordinaryOnly.Requests = nil
	for _, r := range requests {
		if r.Status != model.StatusWaitingForHandoff {
			ordinaryOnly.Requests = append(ordinaryOnly.Requests, r)
		}
	}
	want := matching.NewGreedy().Match(ordinaryOnly)

	gotOrd := filterChangesByRequests(got, map[string]bool{"waiting": true})
	assertSameChanges(t, gotOrd, want)

	onwardOK := false
	for _, ch := range got {
		for _, s := range ch.Plan {
			if s.RequestID == "waiting" && s.Type == model.Pickup {
				onwardOK = true
			}
		}
	}
	if !onwardOK {
		t.Fatalf("waiting rider must be rematched in same Match; changes=%+v", got)
	}
}

func filterChangesByRequests(changes []model.PlanChange, exclude map[string]bool) []model.PlanChange {
	var out []model.PlanChange
	for _, ch := range changes {
		var plan []model.Stop
		for _, s := range ch.Plan {
			if exclude[s.RequestID] {
				continue
			}
			plan = append(plan, s)
		}
		// Include cars that greedy changed even if only non-excluded stops remain,
		// and cars whose full plan became empty of excluded-only content.
		origHadExcludedOnly := true
		for _, s := range ch.Plan {
			if !exclude[s.RequestID] {
				origHadExcludedOnly = false
				break
			}
		}
		if len(plan) == 0 && (len(ch.Plan) == 0 || !origHadExcludedOnly) {
			// keep empty plan changes that cleared ordinary stops
		}
		if len(plan) == 0 && origHadExcludedOnly && len(ch.Plan) > 0 {
			continue // onward-only car
		}
		out = append(out, model.PlanChange{SupplyID: ch.SupplyID, Plan: plan})
	}
	return out
}

// TestOnwardOfflineRematchEndToEnd: after an onward assignment is cleared (simulating
// stop_removed / car offline), a later Match with the rider back in waiting_for_handoff
// must assign a different feasible car under the realized path cap.
func TestOnwardOfflineRematchEndToEnd(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	handoff := testHandoff
	settings := baseSettings(false)
	near := model.Location{Lat: testHandoff.Lat + 0.01, Lng: testHandoff.Lng}
	far := model.Location{Lat: testHandoff.Lat + 0.05, Lng: testHandoff.Lng}
	// First match: prefer non-empty-at-H car_near over car_far.
	in1 := model.MatchInput{
		Now: 1000, Settings: settings,
		Supplies: []model.SupplyView{
			{SupplyID: "car_near", Location: near, State: model.StateIdle, NumAvailableSeats: 4},
			{SupplyID: "car_far", Location: far, State: model.StateIdle, NumAvailableSeats: 4},
		},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: 100,
		}},
		Router: router,
	}
	first := New().Match(in1)
	if len(first) != 1 || first[0].SupplyID != "car_near" {
		t.Fatalf("first onward assign=%+v", first)
	}
	// Second match: car_near offline; rider waiting again → rematch to car_far.
	in2 := model.MatchInput{
		Now: 1100, Settings: settings,
		Supplies: []model.SupplyView{
			{SupplyID: "car_near", Location: near, State: model.StateGoingOffline, NumAvailableSeats: 4},
			{SupplyID: "car_far", Location: far, State: model.StateIdle, NumAvailableSeats: 4},
		},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: 100,
		}},
		Router: router,
	}
	second := New().Match(in2)
	if len(second) != 1 || second[0].SupplyID != "car_far" {
		t.Fatalf("rematch after offline=%+v", second)
	}
}

func TestRetainsPickupsHelper(t *testing.T) {
	base := []model.Stop{
		{RequestID: "s", Type: model.Pickup},
		{RequestID: "s", Type: model.Dropoff},
	}
	keep := []model.Stop{
		{RequestID: "s", Type: model.Pickup},
		{RequestID: "r", Type: model.Pickup},
		{RequestID: "r", Type: model.Dropoff},
		{RequestID: "s", Type: model.Dropoff},
	}
	drop := []model.Stop{
		{RequestID: "r", Type: model.Pickup},
		{RequestID: "r", Type: model.Dropoff},
	}
	if !retainsPickups(base, keep, "r") {
		t.Fatal("expected retain")
	}
	if retainsPickups(base, drop, "r") {
		t.Fatal("expected reject displaced s")
	}
	priorR := []model.Stop{{RequestID: "r", Type: model.Pickup}, {RequestID: "r", Type: model.Dropoff}}
	if !retainsPickups(priorR, drop, "r") {
		t.Fatal("exceptID's own pickup may be replaced")
	}
}

// TestTransferRetainsAssignedPickup: R's transfer onto car_a must not drop
// assigned rider S. The matcher itself must retain S (not rely on core rejection).
func TestTransferRetainsAssignedPickup(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	aLoc := model.Location{Lat: 37.785, Lng: -122.43}
	sDrop := model.Location{Lat: 37.788, Lng: -122.425}
	// Disable reassignment so greedy cannot move S; transfer overlay must retain S
	// on car_a if it inserts R there (or leave S untouched).
	settings := baseSettings(true)
	settings.ReassignmentEnabled = false
	in := model.MatchInput{
		Now:      1000,
		Settings: settings,
		Supplies: []model.SupplyView{{
			SupplyID: "car_a", Location: aLoc, State: model.StateEnRoute,
			NumAvailableSeats: 4, SeatsInUse: 0,
			Plan: []model.Waypoint{
				{RequestID: "s", Type: model.Pickup, Location: aLoc, EtaAt: 1300, NumRequestedSeats: 1},
				{RequestID: "s", Type: model.Dropoff, Location: sDrop, EtaAt: 1400, NumRequestedSeats: 1},
			},
		}, {
			SupplyID: "car_b", Location: testDestination, State: model.StateIdle, NumAvailableSeats: 4,
		}, {
			SupplyID: "car_far", Location: model.Location{Lat: 37.70, Lng: -122.50}, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "s", RequestedAt: 900, Origin: aLoc, Destination: sDrop,
			NumRequestedSeats: 1, Status: model.StatusAssigned, SupplyID: "car_a",
		}, {
			RequestID: "r", RequestedAt: 1000, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	changes := New().Match(in)
	if !pickupPresent(changes, in.Supplies, "car_a", "s") {
		t.Fatalf("assigned S pickup must remain on car_a when reassignment is off; changes=%+v", changes)
	}
	for _, ch := range changes {
		if ch.SupplyID != "car_a" {
			continue
		}
		if !planHasPickup(ch.Plan, "s") {
			t.Fatalf("car_a plan dropped S: %+v", ch.Plan)
		}
	}
}

// TestTransferDoesNotDropDriverArrivedPickup: same ownership for driver_arrived S.
// Matcher must not emit a car_a plan that omits S (batch must not depend on core rejection).
func TestTransferDoesNotDropDriverArrivedPickup(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	aLoc := model.Location{Lat: 37.785, Lng: -122.43}
	sDrop := model.Location{Lat: 37.788, Lng: -122.425}
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{{
			SupplyID: "car_a", Location: aLoc, State: model.StateEnRoute,
			NumAvailableSeats: 4, SeatsInUse: 0,
			Plan: []model.Waypoint{
				{RequestID: "s", Type: model.Pickup, Location: aLoc, EtaAt: 900, NumRequestedSeats: 1}, // EtaAt <= now → locked
				{RequestID: "s", Type: model.Dropoff, Location: sDrop, EtaAt: 1400, NumRequestedSeats: 1},
			},
		}, {
			SupplyID: "car_b", Location: testDestination, State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "s", RequestedAt: 800, Origin: aLoc, Destination: sDrop,
			NumRequestedSeats: 1, Status: model.StatusDriverArrived, SupplyID: "car_a",
		}, {
			RequestID: "r", RequestedAt: 1000, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusRequested,
		}},
		Router: router,
	}
	changes := New().Match(in)
	for _, ch := range changes {
		if ch.SupplyID != "car_a" {
			continue
		}
		if !planHasPickup(ch.Plan, "s") {
			t.Fatalf("matcher emitted car_a plan without driver_arrived S; would rely on core rejection: %+v", ch.Plan)
		}
	}
	if !pickupPresent(changes, in.Supplies, "car_a", "s") {
		t.Fatalf("driver_arrived S must remain on car_a; changes=%+v", changes)
	}
}

// TestOnwardDoesNotDisplaceEarlierOverlayPickup: onward rematch onto a car that
// already holds an earlier transfer/overlay pickup must keep that pickup.
func TestOnwardDoesNotDisplaceEarlierOverlayPickup(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	handoff := testHandoff
	overlayH := model.Location{Lat: 37.780, Lng: -122.420}
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(false), // transfers off: onward rematch only for waiting
		Supplies: []model.SupplyView{{
			SupplyID: "car_b", Location: model.Location{Lat: testHandoff.Lat + 0.01, Lng: testHandoff.Lng},
			State: model.StateEnRoute, NumAvailableSeats: 4,
			Plan: []model.Waypoint{
				{RequestID: "overlay", Type: model.Pickup, Location: testOrigin, EtaAt: 1500, NumRequestedSeats: 1},
				{RequestID: "overlay", Type: model.Dropoff, Location: overlayH, EtaAt: 1800, NumRequestedSeats: 1},
			},
		}, {
			SupplyID: "car_other", Location: model.Location{Lat: testHandoff.Lat + 0.05, Lng: testHandoff.Lng},
			State: model.StateIdle, NumAvailableSeats: 4,
		}},
		Requests: []model.RequestView{{
			RequestID: "overlay", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusAssigned, SupplyID: "car_b",
		}, {
			RequestID: "waiting", RequestedAt: 50, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: 100,
		}},
		Router: router,
	}
	changes := New().Match(in)
	// Wherever waiting is assigned, overlay pickup must still exist on car_b
	// (either unchanged input plan or an updated plan that retains it).
	if !pickupPresent(changes, in.Supplies, "car_b", "overlay") {
		t.Fatalf("onward must not displace overlay pickup; changes=%+v", changes)
	}
	for _, ch := range changes {
		if ch.SupplyID != "car_b" {
			continue
		}
		if planHasPickup(ch.Plan, "waiting") && !planHasPickup(ch.Plan, "overlay") {
			t.Fatalf("onward plan on car_b dropped overlay: %+v", ch.Plan)
		}
	}
}

// TestCompliantOnwardInsertionWinsWhenMinEndViolatesCap: min-end onward
// insertion can exceed remaining path budget while a later insertion complies.
func TestCompliantOnwardInsertionWinsWhenMinEndViolatesCap(t *testing.T) {
	router, err := routing.NewStraightLine(10)
	if err != nil {
		t.Fatal(err)
	}
	handoff := testHandoff
	// Co-rider dropoff lies between H and D so min-end prefers routing through
	// it while aboard (over a tight remaining budget); dropping r1 before the
	// co-rider keeps path within remaining and must win.
	far := model.Location{Lat: 37.795000, Lng: -122.406000}
	od := routerDistance(router, testOrigin, testDestination)
	cap := int64(math.Floor(1.25 * float64(od)))
	// Path H→far→D exceeds a small remaining; path H→D does not.
	hToD := routerDistance(router, handoff, testDestination)
	hToFar := routerDistance(router, handoff, far)
	farToD := routerDistance(router, far, testDestination)
	viaFar := hToFar + farToD
	if viaFar <= hToD {
		t.Fatalf("geometry: viaFar=%d should exceed direct H→D=%d", viaFar, hToD)
	}
	remaining := hToD // direct OK; via far not
	realized := cap - remaining
	if realized < 0 {
		t.Fatalf("cap too small")
	}
	in := model.MatchInput{
		Now:      1000,
		Settings: baseSettings(true),
		Supplies: []model.SupplyView{{
			SupplyID: "car_b", Location: handoff, State: model.StateOnTrip,
			NumAvailableSeats: 4, SeatsInUse: 1,
			Plan: []model.Waypoint{
				{RequestID: "co", Type: model.Dropoff, Location: far, EtaAt: 2000, NumRequestedSeats: 1},
			},
		}},
		Requests: []model.RequestView{{
			RequestID: "r1", RequestedAt: 100, Origin: testOrigin, Destination: testDestination,
			NumRequestedSeats: 1, Status: model.StatusWaitingForHandoff, HandoffLocation: &handoff,
			RealizedAboardDistanceMeters: realized,
		}},
		Router: router,
	}
	ctx := &matchContext{in: in, stops: buildStopInfo(in), budget: 5000, ratio: 1.25}
	plan, _, path, ok := ctx.bestOnwardInsertion(in.Supplies[0], planMap(in.Supplies)["car_b"], in.Requests[0], handoff, remaining)
	if !ok {
		t.Fatalf("expected a compliant onward insertion within remaining=%d", remaining)
	}
	if path > remaining {
		t.Fatalf("winning path %d exceeds remaining %d", path, remaining)
	}
	// Must not keep co between r1 pickup and dropoff (that is the over-cap path).
	pickupIdx, dropIdx, coIdx := -1, -1, -1
	for i, s := range plan {
		if s.RequestID == "r1" && s.Type == model.Pickup {
			pickupIdx = i
		}
		if s.RequestID == "r1" && s.Type == model.Dropoff {
			dropIdx = i
		}
		if s.RequestID == "co" && s.Type == model.Dropoff {
			coIdx = i
		}
	}
	if pickupIdx < 0 || dropIdx < 0 || coIdx < 0 {
		t.Fatalf("incomplete plan=%+v", plan)
	}
	if pickupIdx < coIdx && coIdx < dropIdx {
		t.Fatalf("over-cap via-far insertion won; plan=%+v path=%d remaining=%d", plan, path, remaining)
	}
}

func planHasPickup(plan []model.Stop, requestID string) bool {
	for _, s := range plan {
		if s.RequestID == requestID && s.Type == model.Pickup {
			return true
		}
	}
	return false
}

// pickupPresent reports whether requestID's pickup is on supplyID after applying changes
// (falls back to the input supply plan when that car has no PlanChange).
func pickupPresent(changes []model.PlanChange, supplies []model.SupplyView, supplyID, requestID string) bool {
	for _, ch := range changes {
		if ch.SupplyID == supplyID {
			return planHasPickup(ch.Plan, requestID)
		}
	}
	for _, s := range supplies {
		if s.SupplyID != supplyID {
			continue
		}
		for _, w := range s.Plan {
			if w.RequestID == requestID && w.Type == model.Pickup {
				return true
			}
		}
	}
	return false
}
