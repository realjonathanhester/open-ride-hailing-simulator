package tests

import (
	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

var (
	unionSquare = model.Location{Lat: 37.787994, Lng: -122.407437}
	coitTower   = model.Location{Lat: 37.802395, Lng: -122.405822}
)

func loc(l model.Location) *model.Location { return &l }

func init() {
	mustRegisterHandoff("two_car_handoff_matcher", twoCarHandoffMatcher{})
	mustRegisterHandoff("invalid_onward_location_matcher", invalidOnwardLocationMatcher{})
	mustRegisterHandoff("duplicate_onward_matcher", duplicateOnwardMatcher{})
	mustRegisterHandoff("handoff_offline_no_rematch_matcher", handoffOfflineMatcher{rematch: false})
	mustRegisterHandoff("handoff_offline_rematch_matcher", handoffOfflineMatcher{rematch: true})
	mustRegisterHandoff("capacity_handoff_matcher", capacityHandoffMatcher{})
	mustRegisterHandoff("reject_second_car_matcher", rejectSecondCarMatcher{})
	mustRegisterHandoff("matcher_drop_onward_matcher", &matcherDropOnwardMatcher{})
	mustRegisterHandoff("handoff_then_greedy_matcher", handoffThenGreedyMatcher{})
	mustRegisterHandoff("third_party_before_handoff_matcher", thirdPartyBeforeHandoffMatcher{})
	mustRegisterHandoff("pooled_cancel_onward_matcher", pooledCancelOnwardMatcher{})
	mustRegisterHandoff("first_leg_handoff_only_matcher", firstLegHandoffOnlyMatcher{})
}

func mustRegisterHandoff(name string, m model.Matcher) {
	if err := runner.RegisterMatcher(name, func() (model.Matcher, error) { return m, nil }); err != nil {
		panic(err)
	}
}

func requestID(in model.MatchInput) string {
	for _, r := range in.Requests {
		if r.Status == model.StatusRequested || r.Status == model.StatusWaitingForHandoff ||
			r.Status == model.StatusAssigned || r.Status == model.StatusDriverArrived {
			return r.RequestID
		}
	}
	if len(in.Requests) > 0 {
		return in.Requests[0].RequestID
	}
	return ""
}

func hasPlan(in model.MatchInput, supplyID string) bool {
	for _, s := range in.Supplies {
		if s.SupplyID == supplyID && len(s.Plan) > 0 {
			return true
		}
	}
	return false
}

func findRequest(in model.MatchInput, id string) (model.RequestView, bool) {
	for _, r := range in.Requests {
		if r.RequestID == id {
			return r, true
		}
	}
	return model.RequestView{}, false
}

func supplyPlan(in model.MatchInput, supplyID string) []model.Waypoint {
	for _, s := range in.Supplies {
		if s.SupplyID == supplyID {
			return s.Plan
		}
	}
	return nil
}

// twoCarHandoffMatcher is two-phase: first-leg handoff only, then onward after
// the rider is waiting_for_handoff (v1 installs onward only after dropoff).
type twoCarHandoffMatcher struct{}

func (twoCarHandoffMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	id := ""
	for _, r := range in.Requests {
		if r.Status == model.StatusWaitingForHandoff {
			id = r.RequestID
			second := "car_at_union_square"
			for _, s := range in.Supplies {
				if s.SupplyID == "car_at_ferry_building" && len(s.Plan) == 0 {
					second = "car_at_ferry_building"
					break
				}
			}
			for _, s := range in.Supplies {
				if s.SupplyID == second && len(s.Plan) == 0 {
					return []model.PlanChange{{SupplyID: second, Plan: []model.Stop{
						{RequestID: id, Type: model.Pickup, Location: handoff},
						{RequestID: id, Type: model.Dropoff},
					}}}
				}
			}
			return nil
		}
		if r.Status == model.StatusRequested {
			id = r.RequestID
		}
	}
	if id == "" || hasPlan(in, "car_at_kezar_stadium") {
		return nil
	}
	return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
		{RequestID: id, Type: model.Pickup},
		{RequestID: id, Type: model.Dropoff, Location: handoff},
	}}}
}

// invalidOnwardLocationMatcher proposes a waiting rider pickup at the wrong place.
type invalidOnwardLocationMatcher struct{}

func (invalidOnwardLocationMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	wrong := loc(coitTower)
	for _, r := range in.Requests {
		if r.Status == model.StatusWaitingForHandoff {
			return []model.PlanChange{{SupplyID: "car_at_union_square", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup, Location: wrong},
				{RequestID: r.RequestID, Type: model.Dropoff},
			}}}
		}
		if r.Status == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
			return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup},
				{RequestID: r.RequestID, Type: model.Dropoff, Location: handoff},
			}}}
		}
	}
	return nil
}

// duplicateOnwardMatcher proposes two onward pickups for one waiting rider.
type duplicateOnwardMatcher struct{}

func (duplicateOnwardMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	for _, r := range in.Requests {
		if r.Status == model.StatusWaitingForHandoff {
			id := r.RequestID
			return []model.PlanChange{
				{SupplyID: "car_at_union_square", Plan: []model.Stop{
					{RequestID: id, Type: model.Pickup, Location: handoff},
					{RequestID: id, Type: model.Dropoff},
				}},
				{SupplyID: "car_at_ferry_building", Plan: []model.Stop{
					{RequestID: id, Type: model.Pickup, Location: handoff},
					{RequestID: id, Type: model.Dropoff},
				}},
			}
		}
		if r.Status == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
			return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup},
				{RequestID: r.RequestID, Type: model.Dropoff, Location: handoff},
			}}}
		}
	}
	return nil
}

type handoffOfflineMatcher struct{ rematch bool }

func (m handoffOfflineMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	for _, r := range in.Requests {
		if r.Status == model.StatusWaitingForHandoff {
			if r.SupplyID != "" {
				return nil // already assigned onward
			}
			// Prefer the ferry car while it is available; rematch uses union after offline.
			for _, s := range in.Supplies {
				if s.SupplyID == "car_at_ferry_building" && len(s.Plan) == 0 && s.State != model.StateGoingOffline && s.State != model.StateOffline {
					return []model.PlanChange{{SupplyID: "car_at_ferry_building", Plan: []model.Stop{
						{RequestID: r.RequestID, Type: model.Pickup, Location: handoff},
						{RequestID: r.RequestID, Type: model.Dropoff},
					}}}
				}
			}
			if m.rematch {
				for _, s := range in.Supplies {
					if s.SupplyID == "car_at_union_square" && len(s.Plan) == 0 {
						return []model.PlanChange{{SupplyID: "car_at_union_square", Plan: []model.Stop{
							{RequestID: r.RequestID, Type: model.Pickup, Location: handoff},
							{RequestID: r.RequestID, Type: model.Dropoff},
						}}}
					}
				}
			}
			return nil
		}
		if r.Status == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
			return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup},
				{RequestID: r.RequestID, Type: model.Dropoff, Location: handoff},
			}}}
		}
	}
	return nil
}

type rejectSecondCarMatcher struct{}

func (rejectSecondCarMatcher) Match(in model.MatchInput) []model.PlanChange {
	// Propose a handoff dropoff plus an invalid same-batch onward (pickup without
	// dropoff) so all-or-nothing rejection still covers linked batches that try
	// to install onward early. With v1 two-phase, also reject a lone invalid
	// waiting pickup.
	handoff := loc(unionSquare)
	for _, r := range in.Requests {
		if r.Status == model.StatusWaitingForHandoff {
			return []model.PlanChange{{SupplyID: "car_at_union_square", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup, Location: handoff},
			}}}
		}
		if r.Status == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
			return []model.PlanChange{
				{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
					{RequestID: r.RequestID, Type: model.Pickup},
					{RequestID: r.RequestID, Type: model.Dropoff, Location: handoff},
				}},
				{SupplyID: "car_at_union_square", Plan: []model.Stop{
					{RequestID: r.RequestID, Type: model.Pickup, Location: handoff},
				}},
			}
		}
	}
	return nil
}

type capacityHandoffMatcher struct{}

func (capacityHandoffMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	var rider1, rider2 string
	var rider1Status, rider2Status string
	for _, r := range in.Requests {
		switch r.RequestID {
		case "kezar_stadium_to_coit_tower":
			rider1, rider1Status = r.RequestID, r.Status
		case "painted_ladies_to_union_square":
			rider2, rider2Status = r.RequestID, r.Status
		}
	}
	if rider1Status == model.StatusWaitingForHandoff {
		for _, s := range in.Supplies {
			if s.SupplyID == "car_at_union_square" && len(s.Plan) == 0 {
				return []model.PlanChange{{SupplyID: "car_at_union_square", Plan: []model.Stop{
					{RequestID: rider1, Type: model.Pickup, Location: handoff},
					{RequestID: rider1, Type: model.Dropoff},
				}}}
			}
		}
	}
	if rider1Status == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
		return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
			{RequestID: rider1, Type: model.Pickup},
			{RequestID: rider1, Type: model.Dropoff, Location: handoff},
		}}}
	}
	if rider2Status == model.StatusRequested {
		for _, s := range in.Supplies {
			if s.SupplyID == "car_at_kezar_stadium" && s.SeatsInUse == 0 && len(s.Plan) == 0 {
				return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
					{RequestID: rider2, Type: model.Pickup},
					{RequestID: rider2, Type: model.Dropoff},
				}}}
			}
		}
	}
	return nil
}

// matcherDropOnwardMatcher assigns onward to ferry, then on a later call drops
// that pickup (reassignment) without giving another car — stranding unless core
// clears assignment fields and logs stop_removed.
type matcherDropOnwardMatcher struct {
	dropped bool
}

func (m *matcherDropOnwardMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	for _, r := range in.Requests {
		if r.Status == model.StatusWaitingForHandoff && r.SupplyID == "" {
			return []model.PlanChange{{SupplyID: "car_at_ferry_building", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup, Location: handoff},
				{RequestID: r.RequestID, Type: model.Dropoff},
			}}}
		}
		if r.Status == model.StatusAssigned && r.SupplyID == "car_at_ferry_building" && r.HandoffLocation != nil {
			// Drop the onward pickup via an empty plan for the ferry car.
			return []model.PlanChange{{SupplyID: "car_at_ferry_building", Plan: nil}}
		}
		if r.Status == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
			return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup},
				{RequestID: r.RequestID, Type: model.Dropoff, Location: handoff},
			}}}
		}
	}
	return nil
}

// handoffThenGreedyMatcher does the first-leg handoff, then delegates to greedy
// so an existing handoff plan on kezar must be preserved across calls.
type handoffThenGreedyMatcher struct{}

func (handoffThenGreedyMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	for _, r := range in.Requests {
		if r.Status == model.StatusWaitingForHandoff {
			return matching.NewGreedy().Match(in)
		}
		if r.Status == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
			return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup},
				{RequestID: r.RequestID, Type: model.Dropoff, Location: handoff},
			}}}
		}
	}
	// Rider on_trip: still call greedy so it must preserve the handoff dropoff location.
	if hasPlan(in, "car_at_kezar_stadium") {
		return matching.NewGreedy().Match(in)
	}
	return nil
}

// thirdPartyBeforeHandoffMatcher lets the union car take an unrelated trip
// before the handoff rider needs it, then assigns onward after waiting.
type thirdPartyBeforeHandoffMatcher struct{}

func (thirdPartyBeforeHandoffMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	var handoffRider, other string
	var handoffStatus, otherStatus string
	for _, r := range in.Requests {
		switch r.RequestID {
		case "kezar_stadium_to_coit_tower":
			handoffRider, handoffStatus = r.RequestID, r.Status
		case "painted_ladies_to_union_square":
			other, otherStatus = r.RequestID, r.Status
		}
	}
	if otherStatus == model.StatusRequested && !hasPlan(in, "car_at_union_square") {
		return []model.PlanChange{{SupplyID: "car_at_union_square", Plan: []model.Stop{
			{RequestID: other, Type: model.Pickup},
			{RequestID: other, Type: model.Dropoff},
		}}}
	}
	if handoffStatus == model.StatusWaitingForHandoff {
		for _, s := range in.Supplies {
			if s.SupplyID == "car_at_union_square" && len(s.Plan) == 0 && s.SeatsInUse == 0 {
				return []model.PlanChange{{SupplyID: "car_at_union_square", Plan: []model.Stop{
					{RequestID: handoffRider, Type: model.Pickup, Location: handoff},
					{RequestID: handoffRider, Type: model.Dropoff},
				}}}
			}
		}
		return nil
	}
	if handoffStatus == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
		return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
			{RequestID: handoffRider, Type: model.Pickup},
			{RequestID: handoffRider, Type: model.Dropoff, Location: handoff},
		}}}
	}
	return nil
}

// pooledCancelOnwardMatcher: kezar handoffs rider1; ferry picks up rider1 onward
// while already carrying rider2 (pooled). Cancel rider1 must keep rider2's dropoff.
type pooledCancelOnwardMatcher struct{}

func (pooledCancelOnwardMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	var r1, r2 string
	var s1, s2 string
	for _, r := range in.Requests {
		switch r.RequestID {
		case "kezar_stadium_to_coit_tower":
			r1, s1 = r.RequestID, r.Status
		case "ferry_building_to_coit_tower":
			r2, s2 = r.RequestID, r.Status
		}
	}
	if s2 == model.StatusRequested && !hasPlan(in, "car_at_ferry_building") {
		return []model.PlanChange{{SupplyID: "car_at_ferry_building", Plan: []model.Stop{
			{RequestID: r2, Type: model.Pickup},
			{RequestID: r2, Type: model.Dropoff},
		}}}
	}
	if s1 == model.StatusWaitingForHandoff {
		for _, s := range in.Supplies {
			if s.SupplyID != "car_at_ferry_building" {
				continue
			}
			// Insert onward pickup/dropoff around existing plan / aboard dropoff.
			var plan []model.Stop
			for _, w := range s.Plan {
				locCopy := w.Location
				plan = append(plan, model.Stop{RequestID: w.RequestID, Type: w.Type, Location: &locCopy})
			}
			plan = append(plan,
				model.Stop{RequestID: r1, Type: model.Pickup, Location: handoff},
				model.Stop{RequestID: r1, Type: model.Dropoff},
			)
			return []model.PlanChange{{SupplyID: "car_at_ferry_building", Plan: plan}}
		}
	}
	if s1 == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
		return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
			{RequestID: r1, Type: model.Pickup},
			{RequestID: r1, Type: model.Dropoff, Location: handoff},
		}}}
	}
	return nil
}

// firstLegHandoffOnlyMatcher installs a handoff dropoff at Union Square and
// never assigns an onward car, so the handoff wait governs resolution.
type firstLegHandoffOnlyMatcher struct{}

func (firstLegHandoffOnlyMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	for _, r := range in.Requests {
		if r.Status == model.StatusRequested && !hasPlan(in, "car_at_kezar_stadium") {
			return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
				{RequestID: r.RequestID, Type: model.Pickup},
				{RequestID: r.RequestID, Type: model.Dropoff, Location: handoff},
			}}}
		}
	}
	return nil
}

// captureRealizedDistanceMatcher records RealizedAboardDistanceMeters before and
// after the handoff dropoff, and performs a two-car handoff like twoCarHandoffMatcher.
var captureRealized = &realizedCapture{}

type realizedCapture struct {
	beforeHandoff int64
	afterHandoff  int64
	sawWaiting    bool
}

func (c *realizedCapture) reset() {
	c.beforeHandoff, c.afterHandoff, c.sawWaiting = 0, 0, false
}

func init() {
	mustRegisterHandoff("capture_realized_distance_matcher", captureRealizedDistanceMatcher{})
}

type captureRealizedDistanceMatcher struct{}

func (captureRealizedDistanceMatcher) Match(in model.MatchInput) []model.PlanChange {
	handoff := loc(unionSquare)
	for _, r := range in.Requests {
		if r.Status == model.StatusWaitingForHandoff {
			captureRealized.sawWaiting = true
			captureRealized.afterHandoff = r.RealizedAboardDistanceMeters
			for _, s := range in.Supplies {
				if s.SupplyID == "car_at_union_square" && len(s.Plan) == 0 {
					return []model.PlanChange{{SupplyID: s.SupplyID, Plan: []model.Stop{
						{RequestID: r.RequestID, Type: model.Pickup, Location: handoff},
						{RequestID: r.RequestID, Type: model.Dropoff},
					}}}
				}
			}
			return nil
		}
		if r.Status == model.StatusRequested || r.Status == model.StatusAssigned || r.Status == model.StatusDriverArrived {
			if r.RealizedAboardDistanceMeters != 0 && !captureRealized.sawWaiting {
				captureRealized.beforeHandoff = r.RealizedAboardDistanceMeters
			}
			if r.Status == model.StatusRequested {
				captureRealized.beforeHandoff = r.RealizedAboardDistanceMeters
			}
		}
	}
	id := ""
	for _, r := range in.Requests {
		if r.Status == model.StatusRequested {
			id = r.RequestID
			captureRealized.beforeHandoff = r.RealizedAboardDistanceMeters
		}
	}
	if id == "" || hasPlan(in, "car_at_kezar_stadium") {
		return nil
	}
	return []model.PlanChange{{SupplyID: "car_at_kezar_stadium", Plan: []model.Stop{
		{RequestID: id, Type: model.Pickup},
		{RequestID: id, Type: model.Dropoff, Location: handoff},
	}}}
}
