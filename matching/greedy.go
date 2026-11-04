// Package matching holds the built-in greedy nearest-ETA matcher.
//
// It is the reference_test_matcher described in docs/test_specs.md:
//  1. Open requests are considered in requested_at order, then request_id.
//     Assigned (not yet arrived-at) requests are reconsidered only when
//     reassignment is enabled.
//  2. Every online supply that is not going_offline is evaluated in supply_id
//     order. Pooling off: pickup and dropoff are appended to the end of the
//     plan (forward dispatch). Pooling on: every insertion that keeps the
//     existing stop order and seat limits is tried, keeping the lowest total
//     plan duration, ties to the earliest positions.
//  3. The car with the earliest pickup ETA wins, ties to the lowest supply_id.
//     An assigned request moves only to a strictly earlier car.
//  4. A plan change is returned for every car whose plan changed.
//
// With [matching] max_candidate_cars = N > 0, each request prices only the N
// open cars (empty plan) and the N busy cars (non-empty plan) closest to its
// pickup by haversine distance from the car's current position, ties to the
// lowest supply_id, and prefetches router legs for just those cars.
package matching

import (
	"math"
	"sort"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/routing"
)

// Greedy is the greedy nearest-ETA matcher.
type Greedy struct{}

// NewGreedy returns the matcher.
func NewGreedy() *Greedy { return &Greedy{} }

type stopInfo struct {
	location model.Location
	seats    int
}

type matchContext struct {
	in    model.MatchInput
	stops map[model.Stop]stopInfo
}

// Match implements model.Matcher.
func (g *Greedy) Match(in model.MatchInput) []model.PlanChange {
	ctx := &matchContext{in: in, stops: map[model.Stop]stopInfo{}}
	supplies := map[string]model.SupplyView{}
	work := map[string][]model.Stop{}
	original := map[string][]model.Stop{}
	var eligible []model.SupplyView
	for _, s := range in.Supplies {
		supplies[s.SupplyID] = s
		var plan []model.Stop
		for _, w := range s.Plan {
			stop := model.Stop{RequestID: w.RequestID, Type: w.Type}
			plan = append(plan, stop)
			ctx.stops[stop] = stopInfo{w.Location, w.NumRequestedSeats}
		}
		work[s.SupplyID] = plan
		original[s.SupplyID] = plan
		if s.State != model.StateGoingOffline && s.State != model.StateOffline {
			eligible = append(eligible, s)
		}
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].SupplyID < eligible[j].SupplyID })

	requests := append([]model.RequestView(nil), in.Requests...)
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].RequestedAt != requests[j].RequestedAt {
			return requests[i].RequestedAt < requests[j].RequestedAt
		}
		return requests[i].RequestID < requests[j].RequestID
	})
	for _, r := range requests {
		ctx.stops[model.Stop{RequestID: r.RequestID, Type: model.Pickup}] = stopInfo{r.Origin, r.NumRequestedSeats}
		ctx.stops[model.Stop{RequestID: r.RequestID, Type: model.Dropoff}] = stopInfo{r.Destination, r.NumRequestedSeats}
	}

	limit := in.Settings.MaxCandidateCars
	if limit <= 0 {
		ctx.prefetch(eligible, work, requests)
	} else {
		ctx.prefetchPruned(eligible, work, requests, supplies, limit)
	}
	for _, r := range requests {
		switch r.Status {
		case model.StatusRequested:
		case model.StatusAssigned:
			if !in.Settings.ReassignmentEnabled {
				continue
			}
		default:
			continue
		}
		current := ""
		currentEta := int64(math.MaxInt64)
		if r.Status == model.StatusAssigned {
			current = r.SupplyID
			if s, ok := supplies[current]; ok {
				if etas, _, ok := ctx.evaluate(s, work[current]); ok {
					if eta, found := etas[model.Stop{RequestID: r.RequestID, Type: model.Pickup}]; found {
						currentEta = eta
					}
				}
			}
		}
		candidates := eligible
		if limit > 0 {
			candidates = nearestCandidates(eligible, work, r, current, limit)
		}
		bestSupply, bestEta := "", int64(math.MaxInt64)
		var bestPlan []model.Stop
		for _, s := range candidates {
			if s.SupplyID == current || s.NumAvailableSeats < r.NumRequestedSeats {
				continue
			}
			plan, eta, ok := ctx.bestInsertion(s, work[s.SupplyID], r.RequestID)
			if ok && eta < bestEta {
				bestSupply, bestEta, bestPlan = s.SupplyID, eta, plan
			}
		}
		if bestSupply == "" || bestEta >= currentEta {
			continue
		}
		if current != "" {
			work[current] = without(work[current], r.RequestID)
		}
		work[bestSupply] = bestPlan
	}

	var changes []model.PlanChange
	for _, s := range in.Supplies {
		if !equalPlans(work[s.SupplyID], original[s.SupplyID]) {
			changes = append(changes, model.PlanChange{SupplyID: s.SupplyID, Plan: work[s.SupplyID]})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].SupplyID < changes[j].SupplyID })
	return changes
}

// prefetch hints a many-to-many router, once per call, with every leg the
// open requests' candidate plans can start: from each eligible car, each
// planned stop and each open pickup to each open pickup and dropoff. It
// never changes a result.
func (ctx *matchContext) prefetch(eligible []model.SupplyView, work map[string][]model.Stop, requests []model.RequestView) {
	prefetcher, ok := ctx.in.Router.(model.LegPrefetcher)
	if !ok {
		return
	}
	var sources, destinations []model.Location
	for _, r := range requests {
		if r.Status != model.StatusRequested && (r.Status != model.StatusAssigned || !ctx.in.Settings.ReassignmentEnabled) {
			continue
		}
		sources = append(sources, r.Origin)
		destinations = append(destinations, r.Origin, r.Destination)
	}
	if len(destinations) == 0 {
		return
	}
	for _, s := range eligible {
		sources = append(sources, s.Location)
		for _, stop := range work[s.SupplyID] {
			sources = append(sources, ctx.stops[stop].location)
		}
	}
	prefetcher.Prefetch(sources, destinations, ctx.in.Now)
}

// nearestCandidates returns, in supply_id order, the limit open cars (empty
// working plan) and the limit busy cars (non-empty working plan) closest to
// the request's pickup by haversine distance from the car's current
// position, ties to the lowest supply_id. Cars that can't take the request
// (the current car, too few seats) are left out before ranking.
func nearestCandidates(eligible []model.SupplyView, work map[string][]model.Stop, r model.RequestView, current string, limit int) []model.SupplyView {
	type ranked struct {
		supply   model.SupplyView
		distance int64
	}
	var open, busy []ranked
	for _, s := range eligible {
		if s.SupplyID == current || s.NumAvailableSeats < r.NumRequestedSeats {
			continue
		}
		entry := ranked{s, routing.HaversineMeters(s.Location, r.Origin)}
		if len(work[s.SupplyID]) == 0 {
			open = append(open, entry)
		} else {
			busy = append(busy, entry)
		}
	}
	var out []model.SupplyView
	for _, set := range [][]ranked{open, busy} {
		sort.SliceStable(set, func(i, j int) bool {
			if set[i].distance != set[j].distance {
				return set[i].distance < set[j].distance
			}
			return set[i].supply.SupplyID < set[j].supply.SupplyID
		})
		if len(set) > limit {
			set = set[:limit]
		}
		for _, entry := range set {
			out = append(out, entry.supply)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SupplyID < out[j].SupplyID })
	return out
}

// prefetchPruned hints the router once per call with the legs every open
// request's pruned candidates can use, ranked against the plans as they
// stand at the start of the call. Assignments made during the call can change
// a later request's candidates; those few legs are fetched on demand.
func (ctx *matchContext) prefetchPruned(eligible []model.SupplyView, work map[string][]model.Stop, requests []model.RequestView, supplies map[string]model.SupplyView, limit int) {
	prefetcher, ok := ctx.in.Router.(model.LegPrefetcher)
	if !ok {
		return
	}
	var sources, destinations []model.Location
	seenSource, seenDestination := map[model.Location]bool{}, map[model.Location]bool{}
	addSource := func(l model.Location) {
		if !seenSource[l] {
			seenSource[l] = true
			sources = append(sources, l)
		}
	}
	addDestination := func(l model.Location) {
		if !seenDestination[l] {
			seenDestination[l] = true
			destinations = append(destinations, l)
		}
	}
	for _, r := range requests {
		if r.Status != model.StatusRequested && (r.Status != model.StatusAssigned || !ctx.in.Settings.ReassignmentEnabled) {
			continue
		}
		current := ""
		if r.Status == model.StatusAssigned {
			current = r.SupplyID
		}
		cars := nearestCandidates(eligible, work, r, current, limit)
		if s, ok := supplies[current]; ok {
			cars = append(cars, s)
		}
		for _, l := range []model.Location{r.Origin, r.Destination} {
			addSource(l)
			addDestination(l)
		}
		for _, s := range cars {
			addSource(s.Location)
			for _, stop := range work[s.SupplyID] {
				addSource(ctx.stops[stop].location)
				addDestination(ctx.stops[stop].location)
			}
		}
	}
	if len(destinations) > 0 {
		prefetcher.Prefetch(sources, destinations, ctx.in.Now)
	}
}

// bestInsertion returns the candidate plan with the lowest total duration and
// the request's pickup ETA in it.
func (ctx *matchContext) bestInsertion(s model.SupplyView, plan []model.Stop, requestID string) ([]model.Stop, int64, bool) {
	pickup := model.Stop{RequestID: requestID, Type: model.Pickup}
	dropoff := model.Stop{RequestID: requestID, Type: model.Dropoff}
	n := len(plan)
	firstPosition := 0
	if n > 0 && len(s.Plan) > 0 && s.Plan[0].EtaAt <= ctx.in.Now && s.Plan[0].RequestID == plan[0].RequestID && s.Plan[0].Type == plan[0].Type {
		firstPosition = 1 // the car is already at its first stop (dwelling); keep it first
	}
	var candidates [][]model.Stop
	if !ctx.in.Settings.PoolingEnabled {
		candidates = append(candidates, append(append(append([]model.Stop(nil), plan...), pickup), dropoff))
	} else {
		for i := firstPosition; i <= n; i++ {
			for j := i; j <= n; j++ {
				c := make([]model.Stop, 0, n+2)
				c = append(c, plan[:i]...)
				c = append(c, pickup)
				c = append(c, plan[i:j]...)
				c = append(c, dropoff)
				c = append(c, plan[j:]...)
				candidates = append(candidates, c)
			}
		}
	}
	var best []model.Stop
	bestEnd, bestEta := int64(math.MaxInt64), int64(0)
	for _, c := range candidates {
		etas, end, ok := ctx.evaluate(s, c)
		if ok && end < bestEnd {
			best, bestEnd, bestEta = c, end, etas[pickup]
		}
	}
	return best, bestEta, best != nil
}

// evaluate routes the plan from the car's location and returns each stop's
// arrival time, the time the plan finishes and whether seats allow it.
func (ctx *matchContext) evaluate(s model.SupplyView, plan []model.Stop) (map[model.Stop]int64, int64, bool) {
	inPlan := map[model.Stop]bool{}
	for _, stop := range plan {
		inPlan[stop] = true
	}
	seats, riders := s.SeatsInUse, 0
	for _, stop := range plan {
		if stop.Type == model.Dropoff && !inPlan[model.Stop{RequestID: stop.RequestID, Type: model.Pickup}] {
			riders++
		}
	}
	locations := make([]model.Location, len(plan))
	for i, stop := range plan {
		info := ctx.stops[stop]
		locations[i] = info.location
		if stop.Type == model.Pickup {
			seats += info.seats
			riders++
		} else {
			seats -= info.seats
			riders--
		}
		if seats > s.NumAvailableSeats || (!ctx.in.Settings.PoolingEnabled && riders > 1) {
			return nil, 0, false
		}
	}
	route := ctx.in.Router.Route
	if estimator, ok := ctx.in.Router.(model.LegEstimator); ok {
		route = estimator.Estimate
	}
	legs, err := route(s.Location, locations, ctx.in.Now)
	if err != nil {
		return nil, 0, false
	}
	etas := map[model.Stop]int64{}
	clock := ctx.in.Now
	for i, stop := range plan {
		clock += legs[i].DurationSeconds
		etas[stop] = clock
		if stop.Type == model.Pickup {
			clock += ctx.in.Settings.PickupDwellSeconds
		} else {
			clock += ctx.in.Settings.DropoffDwellSeconds
		}
	}
	return etas, clock, true
}

func without(plan []model.Stop, requestID string) []model.Stop {
	var out []model.Stop
	for _, stop := range plan {
		if stop.RequestID != requestID {
			out = append(out, stop)
		}
	}
	return out
}

func equalPlans(a, b []model.Stop) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
