// Package transfermatcher is the Comparison-B experiment matcher: built-in
// greedy ordinary behavior plus an optional single transfer when a candidate
// passes the rider path-ratio cap and has a strictly earlier pickup ETA.
//
// Ordinary (non-handoff) requests are always matched by delegating to the
// built-in greedy_nearest_eta implementation — there is no second greedy.
// Transfer search and onward rematch are layered on top. When
// transfers_enabled is false, every ordinary request still goes through
// built-in greedy even if an unrelated waiting_for_handoff rider exists.
//
// Handoff-related freeze (v1): any supply whose current plan contains a handoff
// dropoff or an onward pickup is unavailable for new ordinary greedy insertions
// and for new first-leg A / provisional-B / different-onward-B candidates until
// that handoff-related work clears. The currently assigned onward car may keep
// its plan; moving that same rider to another eligible (unfrozen) car is fine.
// Availability cost: cars busy with any transfer leg are excluded from pooling
// new handoff-related work (conservative until rematch/clear).
//
// Transfer and onward insertions must retain every other rider's existing
// pickup in the working plan (greedy and earlier overlays); candidates that
// displace one are skipped so ownership does not depend on core rejection.
package transfermatcher

import (
	"math"
	"sort"

	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/routing"
)

// Name is the [hooks] matching value for this experiment matcher.
const Name = "transfermatcher"

const (
	defaultMaxRiderPathRatio       = 1.25
	defaultMaxInsertionEvaluations = 5000
	// maxPlanStopsForTransfer is P_max: transfer / provisional / onward search
	// skips a supply whose current plan already has more than this many stops.
	maxPlanStopsForTransfer = 8
)

// Matcher is the Comparison-B transfer matcher.
type Matcher struct{}

// New returns the transfer matcher.
func New() *Matcher { return &Matcher{} }

type stopInfo struct {
	location model.Location
	seats    int
}

type matchContext struct {
	in          model.MatchInput
	stops       map[model.Stop]stopInfo
	evaluations int
	budget      int
	ratio       float64
	originOf    map[string]model.Location
	destOf      map[string]model.Location
}

// Match implements model.Matcher.
func (m *Matcher) Match(in model.MatchInput) []model.PlanChange {
	if !in.Settings.HandoffsEnabled {
		return matching.NewGreedy().Match(in)
	}

	transfersOn := transfersEnabled(in.Settings)
	ordinary, onward := splitRequests(in.Requests)
	onwardIDs := onwardRequestIDs(in.Requests)
	originOf, destOf := requestEndpoints(in.Requests)

	// 1) Ordinary requests: always the real built-in greedy (handoff riders excluded).
	// Cars that already hold a handoff dropoff or onward pickup are frozen so
	// greedy cannot insert onto them (v1 path-cap policy).
	greedyIn := in
	greedyIn.Requests = ordinary
	greedyIn.Supplies = freezeHandoffCars(in.Supplies, originOf, destOf, onwardIDs)
	greedyChanges := matching.NewGreedy().Match(greedyIn)

	work := planMap(in.Supplies)
	original := copyPlanMap(work)
	applyPlanChanges(work, greedyChanges)

	ctx := &matchContext{
		in:       in,
		stops:    buildStopInfo(in),
		budget:   in.Settings.MaxInsertionEvaluations,
		ratio:    in.Settings.MaxRiderPathRatio,
		originOf: originOf,
		destOf:   destOf,
	}
	if ctx.budget <= 0 {
		ctx.budget = defaultMaxInsertionEvaluations
	}
	if ctx.ratio <= 0 {
		ctx.ratio = defaultMaxRiderPathRatio
	}

	// 2) Optional transfer overlays on first-leg ordinary requests only.
	if transfersOn {
		ctx.overlayTransfers(work, ordinary, in.Supplies)
	}

	// 3) Onward rematch for waiting / onward-assigned riders (always when handoffs on).
	ctx.assignAllOnward(work, onward, in.Supplies)

	return diffPlans(in.Supplies, original, work)
}

func transfersEnabled(settings model.Settings) bool {
	if settings.TransfersEnabled == nil {
		return true
	}
	return *settings.TransfersEnabled
}

func isOnwardRider(r model.RequestView) bool {
	return r.Status == model.StatusWaitingForHandoff || r.HandoffLocation != nil
}

func splitRequests(requests []model.RequestView) (ordinary, onward []model.RequestView) {
	for _, r := range requests {
		if isOnwardRider(r) {
			onward = append(onward, r)
		} else {
			ordinary = append(ordinary, r)
		}
	}
	return ordinary, onward
}

func onwardRequestIDs(requests []model.RequestView) map[string]bool {
	ids := map[string]bool{}
	for _, r := range requests {
		if r.HandoffLocation != nil {
			ids[r.RequestID] = true
		}
	}
	return ids
}

func requestEndpoints(requests []model.RequestView) (originOf, destOf map[string]model.Location) {
	originOf, destOf = map[string]model.Location{}, map[string]model.Location{}
	for _, r := range requests {
		originOf[r.RequestID] = r.Origin
		destOf[r.RequestID] = r.Destination
	}
	return originOf, destOf
}

// freezeHandoffCars marks supplies whose plan already has a handoff dropoff or
// onward pickup as going_offline for the greedy input only.
func freezeHandoffCars(supplies []model.SupplyView, originOf, destOf map[string]model.Location, onwardIDs map[string]bool) []model.SupplyView {
	out := make([]model.SupplyView, len(supplies))
	copy(out, supplies)
	for i, s := range out {
		if hasHandoffRelatedWaypoints(s.Plan, originOf, destOf, onwardIDs) {
			out[i].State = model.StateGoingOffline
		}
	}
	return out
}

// hasHandoffRelatedWaypoints reports a handoff dropoff (dropoff location ≠ dest)
// or onward pickup (pickup location ≠ origin, or known onward request).
func hasHandoffRelatedWaypoints(plan []model.Waypoint, originOf, destOf map[string]model.Location, onwardIDs map[string]bool) bool {
	for _, w := range plan {
		switch w.Type {
		case model.Dropoff:
			if dest, ok := destOf[w.RequestID]; ok && w.Location != dest {
				return true
			}
		case model.Pickup:
			if onwardIDs[w.RequestID] {
				return true
			}
			if origin, ok := originOf[w.RequestID]; ok && w.Location != origin {
				return true
			}
		}
	}
	return false
}

// hasHandoffRelatedWork reports handoff dropoff / onward pickup on a working plan.
func hasHandoffRelatedWork(plan []model.Stop, originOf, destOf map[string]model.Location) bool {
	for _, s := range plan {
		if s.Location == nil {
			continue
		}
		switch s.Type {
		case model.Dropoff:
			if dest, ok := destOf[s.RequestID]; ok && *s.Location != dest {
				return true
			}
		case model.Pickup:
			if origin, ok := originOf[s.RequestID]; ok && *s.Location != origin {
				return true
			}
		}
	}
	return false
}

func filterUnfrozen(candidates []model.SupplyView, work map[string][]model.Stop, originOf, destOf map[string]model.Location) []model.SupplyView {
	var out []model.SupplyView
	for _, s := range candidates {
		if hasHandoffRelatedWork(work[s.SupplyID], originOf, destOf) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func planMap(supplies []model.SupplyView) map[string][]model.Stop {
	work := map[string][]model.Stop{}
	for _, s := range supplies {
		var plan []model.Stop
		for _, w := range s.Plan {
			loc := w.Location
			plan = append(plan, model.Stop{RequestID: w.RequestID, Type: w.Type, Location: &loc})
		}
		work[s.SupplyID] = plan
	}
	return work
}

func copyPlanMap(work map[string][]model.Stop) map[string][]model.Stop {
	out := map[string][]model.Stop{}
	for id, plan := range work {
		out[id] = append([]model.Stop(nil), plan...)
	}
	return out
}

func applyPlanChanges(work map[string][]model.Stop, changes []model.PlanChange) {
	for _, ch := range changes {
		work[ch.SupplyID] = append([]model.Stop(nil), ch.Plan...)
	}
}

func buildStopInfo(in model.MatchInput) map[model.Stop]stopInfo {
	stops := map[model.Stop]stopInfo{}
	for _, s := range in.Supplies {
		for _, w := range s.Plan {
			stops[model.Stop{RequestID: w.RequestID, Type: w.Type}] = stopInfo{w.Location, w.NumRequestedSeats}
		}
	}
	for _, r := range in.Requests {
		pickup := r.Origin
		if r.HandoffLocation != nil {
			pickup = *r.HandoffLocation
		}
		stops[model.Stop{RequestID: r.RequestID, Type: model.Pickup}] = stopInfo{pickup, r.NumRequestedSeats}
		stops[model.Stop{RequestID: r.RequestID, Type: model.Dropoff}] = stopInfo{r.Destination, r.NumRequestedSeats}
	}
	return stops
}

func diffPlans(supplies []model.SupplyView, original, work map[string][]model.Stop) []model.PlanChange {
	var changes []model.PlanChange
	for _, s := range supplies {
		if !equalPlans(work[s.SupplyID], original[s.SupplyID]) {
			changes = append(changes, model.PlanChange{SupplyID: s.SupplyID, Plan: work[s.SupplyID]})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].SupplyID < changes[j].SupplyID })
	return changes
}

func (ctx *matchContext) atBudget() bool {
	return ctx.evaluations >= ctx.budget
}

// overlayTransfers may replace a greedy first-leg assignment with a transfer
// when the transfer pickup ETA is strictly earlier. All other greedy plan
// changes are preserved. Requests are considered in requested_at / request_id
// order.
func (ctx *matchContext) overlayTransfers(work map[string][]model.Stop, ordinary []model.RequestView, supplies []model.SupplyView) {
	sort.SliceStable(ordinary, func(i, j int) bool {
		if ordinary[i].RequestedAt != ordinary[j].RequestedAt {
			return ordinary[i].RequestedAt < ordinary[j].RequestedAt
		}
		return ordinary[i].RequestID < ordinary[j].RequestID
	})
	supplyByID := map[string]model.SupplyView{}
	var eligible []model.SupplyView
	for _, s := range supplies {
		supplyByID[s.SupplyID] = s
		if s.State != model.StateGoingOffline && s.State != model.StateOffline {
			eligible = append(eligible, s)
		}
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].SupplyID < eligible[j].SupplyID })
	limit := ctx.in.Settings.MaxCandidateCars

	for _, r := range ordinary {
		if ctx.atBudget() {
			break
		}
		switch r.Status {
		case model.StatusRequested:
		case model.StatusAssigned:
			if !ctx.in.Settings.ReassignmentEnabled {
				continue
			}
		default:
			continue
		}
		if r.RealizedAboardDistanceMeters > 0 {
			continue
		}
		currentID, currentEta := assignmentOf(work, supplyByID, r, ctx)
		transferSupply, transferPlan, transferEta := ctx.searchTransfer(eligible, work, r, currentID, limit)
		if transferSupply == "" || transferEta >= currentEta {
			continue
		}
		// Ownership: never install a transfer plan that drops another rider's pickup
		// (greedy or earlier overlay). Skip rather than depending on core rejection.
		if !retainsPickups(work[transferSupply], transferPlan, r.RequestID) {
			continue
		}
		if currentID != "" {
			work[currentID] = without(work[currentID], r.RequestID)
		}
		work[transferSupply] = transferPlan
	}
}

func assignmentOf(work map[string][]model.Stop, supplies map[string]model.SupplyView, r model.RequestView, ctx *matchContext) (string, int64) {
	// Deterministic scan by supply_id.
	ids := make([]string, 0, len(work))
	for id := range work {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		plan := work[id]
		for _, stop := range plan {
			if stop.RequestID == r.RequestID && stop.Type == model.Pickup {
				eta := ctx.peekPickupETA(supplies[id], plan, r)
				return id, eta
			}
		}
	}
	return "", int64(math.MaxInt64)
}

// peekPickupETA estimates pickup ETA without consuming the transfer budget
// (used only to read the greedy baseline assignment).
func (ctx *matchContext) peekPickupETA(s model.SupplyView, plan []model.Stop, r model.RequestView) int64 {
	etas, _, _, ok := ctx.scorePlanCore(s, plan, r.RequestID, false)
	if !ok {
		return int64(math.MaxInt64)
	}
	pickup := model.Stop{RequestID: r.RequestID, Type: model.Pickup}
	if r.HandoffLocation != nil {
		loc := *r.HandoffLocation
		pickup.Location = &loc
	}
	if eta, found := etas[model.StopKey(pickup)]; found {
		return eta
	}
	return int64(math.MaxInt64)
}

func (ctx *matchContext) assignAllOnward(work map[string][]model.Stop, onward []model.RequestView, supplies []model.SupplyView) {
	sort.SliceStable(onward, func(i, j int) bool {
		if onward[i].RequestedAt != onward[j].RequestedAt {
			return onward[i].RequestedAt < onward[j].RequestedAt
		}
		return onward[i].RequestID < onward[j].RequestID
	})
	supplyByID := map[string]model.SupplyView{}
	var eligible []model.SupplyView
	for _, s := range supplies {
		supplyByID[s.SupplyID] = s
		if s.State != model.StateGoingOffline && s.State != model.StateOffline {
			eligible = append(eligible, s)
		}
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].SupplyID < eligible[j].SupplyID })
	limit := ctx.in.Settings.MaxCandidateCars

	for _, r := range onward {
		if r.HandoffLocation == nil {
			continue
		}
		if r.Status == model.StatusAssigned && !ctx.in.Settings.ReassignmentEnabled {
			continue
		}
		ctx.assignOnward(eligible, work, supplyByID, r, limit)
	}
}

func (ctx *matchContext) assignOnward(eligible []model.SupplyView, work map[string][]model.Stop, supplies map[string]model.SupplyView, r model.RequestView, limit int) {
	handoff := *r.HandoffLocation
	current, currentEta := "", int64(math.MaxInt64)
	if r.Status == model.StatusAssigned {
		current = r.SupplyID
		if s, ok := supplies[current]; ok {
			currentEta = ctx.peekPickupETA(s, work[current], r)
		}
	}
	candidates := eligible
	if limit > 0 {
		candidates = nearestCandidates(eligible, work, handoff, r.NumRequestedSeats, current, limit)
	}
	od := ctx.routerDistance(r.Origin, r.Destination)
	cap := int64(math.Floor(ctx.ratio * float64(od)))
	remaining := cap - r.RealizedAboardDistanceMeters

	bestSupply, bestEta := "", int64(math.MaxInt64)
	var bestPlan []model.Stop
	fallbackSupply, fallbackEta := "", int64(math.MaxInt64)
	var fallbackPlan []model.Stop
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].SupplyID < candidates[j].SupplyID })
	for _, s := range candidates {
		if ctx.atBudget() {
			break
		}
		if s.SupplyID == current || s.NumAvailableSeats < r.NumRequestedSeats {
			continue
		}
		if len(work[s.SupplyID]) > maxPlanStopsForTransfer {
			continue
		}
		// v1 freeze: no new onward onto a car that already holds handoff-related work.
		if hasHandoffRelatedWork(work[s.SupplyID], ctx.originOf, ctx.destOf) {
			continue
		}
		if remaining < 0 {
			continue
		}
		plan, eta, _, ok := ctx.bestOnwardInsertion(s, work[s.SupplyID], r, handoff, remaining)
		if !ok {
			continue
		}
		// Prefer cars that are not empty-at-H: during finishStop the just-dropped
		// car is still dwelling with an empty plan and core rejects new plans for it.
		atHandoffEmpty := len(work[s.SupplyID]) == 0 && s.Location == handoff
		if atHandoffEmpty {
			if eta < fallbackEta || (eta == fallbackEta && (fallbackSupply == "" || s.SupplyID < fallbackSupply)) {
				fallbackSupply, fallbackEta, fallbackPlan = s.SupplyID, eta, plan
			}
			continue
		}
		if eta < bestEta || (eta == bestEta && (bestSupply == "" || s.SupplyID < bestSupply)) {
			bestSupply, bestEta, bestPlan = s.SupplyID, eta, plan
		}
	}
	if bestSupply == "" {
		bestSupply, bestEta, bestPlan = fallbackSupply, fallbackEta, fallbackPlan
	}
	if bestSupply == "" || bestEta >= currentEta {
		return
	}
	if !retainsPickups(work[bestSupply], bestPlan, r.RequestID) {
		return
	}
	if current != "" {
		work[current] = without(work[current], r.RequestID)
	}
	work[bestSupply] = bestPlan
}

// searchTransfer iterates deterministic (A, B) pairs. For each H, every
// insertion candidate is scored; the earliest-pickup insertion that leaves a
// nonnegative path remaining AND has a feasible paired-B onward within the
// total cap wins for that (A,B,H).
func (ctx *matchContext) searchTransfer(eligible []model.SupplyView, work map[string][]model.Stop, r model.RequestView, current string, limit int) (string, []model.Stop, int64) {
	aCars := eligible
	if limit > 0 {
		aCars = nearestCandidates(eligible, work, r.Origin, r.NumRequestedSeats, current, limit)
	}
	bCars := eligible
	if limit > 0 {
		bCars = nearestCandidates(eligible, work, r.Destination, r.NumRequestedSeats, "", limit)
	}
	// v1 freeze: exclude cars whose working plan already has a handoff dropoff or
	// onward pickup from new first-leg A and provisional-B candidacy.
	aCars = filterUnfrozen(aCars, work, ctx.originOf, ctx.destOf)
	bCars = filterUnfrozen(bCars, work, ctx.originOf, ctx.destOf)
	sort.SliceStable(aCars, func(i, j int) bool { return aCars[i].SupplyID < aCars[j].SupplyID })
	sort.SliceStable(bCars, func(i, j int) bool { return bCars[i].SupplyID < bCars[j].SupplyID })

	od := ctx.routerDistance(r.Origin, r.Destination)
	cap := int64(math.Floor(ctx.ratio * float64(od)))

	type ranked struct {
		supplyID string
		plan     []model.Stop
		eta      int64
		h        model.Location
	}
	var best *ranked

	for _, a := range aCars {
		if ctx.atBudget() {
			break
		}
		if a.SupplyID == current || a.NumAvailableSeats < r.NumRequestedSeats {
			continue
		}
		if len(work[a.SupplyID]) > maxPlanStopsForTransfer {
			continue
		}
		for _, b := range bCars {
			if ctx.atBudget() {
				break
			}
			if b.SupplyID == a.SupplyID || b.NumAvailableSeats < r.NumRequestedSeats {
				continue
			}
			if len(work[b.SupplyID]) > maxPlanStopsForTransfer {
				continue
			}
			for _, h := range handoffPointsForPair(r.Origin, r.Destination, b.Location) {
				if ctx.atBudget() {
					break
				}
				plan, eta, ok := ctx.bestCompliantHandoffInsertion(a, work[a.SupplyID], b, work[b.SupplyID], r, h, cap)
				if !ok {
					continue
				}
				cand := ranked{a.SupplyID, plan, eta, h}
				if best == nil || cand.eta < best.eta ||
					(cand.eta == best.eta && cand.supplyID < best.supplyID) ||
					(cand.eta == best.eta && cand.supplyID == best.supplyID && (cand.h.Lat < best.h.Lat || (cand.h.Lat == best.h.Lat && cand.h.Lng < best.h.Lng))) {
					cp := cand
					best = &cp
				}
			}
		}
	}
	if best == nil {
		return "", nil, int64(math.MaxInt64)
	}
	return best.supplyID, best.plan, best.eta
}

// bestCompliantHandoffInsertion scores every A insertion for dropoff at H and
// keeps the earliest-pickup plan whose pathAH leaves remaining >= 0 and for
// which B has some onward insertion with pathAH+pathHD <= cap.
func (ctx *matchContext) bestCompliantHandoffInsertion(a model.SupplyView, aPlan []model.Stop, b model.SupplyView, bPlan []model.Stop, r model.RequestView, h model.Location, cap int64) ([]model.Stop, int64, bool) {
	pickup := model.Stop{RequestID: r.RequestID, Type: model.Pickup}
	hCopy := h
	dropoff := model.Stop{RequestID: r.RequestID, Type: model.Dropoff, Location: &hCopy}
	candidates := insertionCandidates(a, aPlan, pickup, dropoff, ctx.in.Now, ctx.in.Settings.PoolingEnabled)

	var best []model.Stop
	bestEta, bestEnd := int64(math.MaxInt64), int64(math.MaxInt64)
	found := false
	for _, c := range candidates {
		if ctx.atBudget() {
			break
		}
		if !retainsPickups(aPlan, c, r.RequestID) {
			continue
		}
		etas, end, pathAH, ok := ctx.scorePlan(a, c, r.RequestID, true)
		if !ok {
			continue
		}
		remaining := cap - pathAH
		if remaining < 0 {
			continue
		}
		if ctx.atBudget() {
			break
		}
		_, _, _, bOK := ctx.bestOnwardInsertion(b, bPlan, r, h, remaining)
		if !bOK {
			continue
		}
		eta := etas[model.StopKey(pickup)]
		if !found || eta < bestEta || (eta == bestEta && end < bestEnd) {
			best, bestEta, bestEnd, found = c, eta, end, true
		}
	}
	return best, bestEta, found
}

func handoffPointsForPair(origin, destination, bLocation model.Location) []model.Location {
	var points []model.Location
	seen := map[model.Location]bool{}
	add := func(p model.Location) {
		if p == origin || p == destination || seen[p] {
			return
		}
		seen[p] = true
		points = append(points, p)
	}
	for _, f := range []float64{1.0 / 3.0, 0.5, 2.0 / 3.0} {
		add(geodesicFraction(origin, destination, f))
	}
	add(bLocation)
	return points
}

func geodesicFraction(a, b model.Location, fraction float64) model.Location {
	return model.Location{
		Lat: a.Lat + (b.Lat-a.Lat)*fraction,
		Lng: a.Lng + (b.Lng-a.Lng)*fraction,
	}
}

func (ctx *matchContext) routerDistance(from, to model.Location) int64 {
	route := ctx.in.Router.Route
	if estimator, ok := ctx.in.Router.(model.LegEstimator); ok {
		route = estimator.Estimate
	}
	legs, err := route(from, []model.Location{to}, ctx.in.Now)
	if err != nil || len(legs) != 1 {
		return routing.HaversineMeters(from, to)
	}
	return legs[0].DistanceMeters
}

// bestOnwardInsertion picks the earliest-pickup (then min-end) insertion whose
// H→…→D path is within remaining, matching first-leg compliant selection.
// Candidates that drop any other rider's existing pickup are skipped.
func (ctx *matchContext) bestOnwardInsertion(s model.SupplyView, plan []model.Stop, r model.RequestView, handoff model.Location, remaining int64) ([]model.Stop, int64, int64, bool) {
	if len(plan) > maxPlanStopsForTransfer {
		return nil, 0, 0, false
	}
	hCopy := handoff
	pickup := model.Stop{RequestID: r.RequestID, Type: model.Pickup, Location: &hCopy}
	dropoff := model.Stop{RequestID: r.RequestID, Type: model.Dropoff}
	ctx.stops[model.StopKey(pickup)] = stopInfo{handoff, r.NumRequestedSeats}
	candidates := insertionCandidates(s, plan, pickup, dropoff, ctx.in.Now, ctx.in.Settings.PoolingEnabled)
	var best []model.Stop
	bestEnd, bestEta, bestPath := int64(math.MaxInt64), int64(math.MaxInt64), int64(0)
	found := false
	for _, c := range candidates {
		if ctx.atBudget() {
			break
		}
		if !retainsPickups(plan, c, r.RequestID) {
			continue
		}
		etas, end, path, ok := ctx.scorePlan(s, c, r.RequestID, true)
		if !ok || path > remaining {
			continue
		}
		eta := etas[model.StopKey(pickup)]
		if !found || eta < bestEta || (eta == bestEta && end < bestEnd) {
			best, bestEnd, bestEta, bestPath, found = c, end, eta, path, true
		}
	}
	return best, bestEta, bestPath, found
}

// retainsPickups reports whether candidate keeps every pickup from base except
// exceptID (the request being inserted / moved). Dropping another rider's
// pickup would silently unassign them when core allows it for assigned (not
// yet driver_arrived) requests under reassignment.
func retainsPickups(base, candidate []model.Stop, exceptID string) bool {
	for _, stop := range base {
		if stop.Type != model.Pickup || stop.RequestID == exceptID {
			continue
		}
		found := false
		for _, c := range candidate {
			if c.Type == model.Pickup && c.RequestID == stop.RequestID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// insertionCandidates mirrors built-in greedy lock rules: first stop is locked
// only when its ETA has already been reached (EtaAt <= now).
func insertionCandidates(s model.SupplyView, plan []model.Stop, pickup, dropoff model.Stop, now int64, pooling bool) [][]model.Stop {
	n := len(plan)
	firstPosition := 0
	if n > 0 && len(s.Plan) > 0 && s.Plan[0].EtaAt <= now && s.Plan[0].RequestID == plan[0].RequestID && s.Plan[0].Type == plan[0].Type {
		firstPosition = 1
	}
	var candidates [][]model.Stop
	if !pooling {
		return append(candidates, append(append(append([]model.Stop(nil), plan...), pickup), dropoff))
	}
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
	return candidates
}

// scorePlan counts against the transfer/onward evaluation budget only.
func (ctx *matchContext) scorePlan(s model.SupplyView, plan []model.Stop, requestID string, needPath bool) (map[model.Stop]int64, int64, int64, bool) {
	if ctx.atBudget() {
		return nil, 0, 0, false
	}
	ctx.evaluations++
	return ctx.scorePlanCore(s, plan, requestID, needPath)
}

func (ctx *matchContext) scorePlanCore(s model.SupplyView, plan []model.Stop, requestID string, needPath bool) (map[model.Stop]int64, int64, int64, bool) {
	inPlan := map[model.Stop]bool{}
	for _, stop := range plan {
		inPlan[model.StopKey(stop)] = true
	}
	seats, riders := s.SeatsInUse, 0
	for _, stop := range plan {
		if stop.Type == model.Dropoff && !inPlan[model.Stop{RequestID: stop.RequestID, Type: model.Pickup}] {
			riders++
		}
	}
	locations := make([]model.Location, len(plan))
	pickupIdx, dropoffIdx := -1, -1
	for i, stop := range plan {
		info := ctx.stops[model.StopKey(stop)]
		locations[i] = info.location
		if stop.Location != nil {
			locations[i] = *stop.Location
		}
		if stop.RequestID == requestID && stop.Type == model.Pickup {
			pickupIdx = i
		}
		if stop.RequestID == requestID && stop.Type == model.Dropoff {
			dropoffIdx = i
		}
		if stop.Type == model.Pickup {
			seats += info.seats
			riders++
		} else {
			seats -= info.seats
			riders--
		}
		if seats > s.NumAvailableSeats || (!ctx.in.Settings.PoolingEnabled && riders > 1) {
			return nil, 0, 0, false
		}
	}
	route := ctx.in.Router.Route
	if estimator, ok := ctx.in.Router.(model.LegEstimator); ok {
		route = estimator.Estimate
	}
	legs, err := route(s.Location, locations, ctx.in.Now)
	if err != nil || len(legs) != len(plan) {
		return nil, 0, 0, false
	}
	etas := map[model.Stop]int64{}
	clock := ctx.in.Now
	for i, stop := range plan {
		clock += legs[i].DurationSeconds
		etas[model.StopKey(stop)] = clock
		if stop.Type == model.Pickup {
			clock += ctx.in.Settings.PickupDwellSeconds
		} else {
			clock += ctx.in.Settings.DropoffDwellSeconds
		}
	}
	var path int64
	if needPath {
		if pickupIdx < 0 || dropoffIdx < 0 || dropoffIdx <= pickupIdx {
			return nil, 0, 0, false
		}
		for i := pickupIdx + 1; i <= dropoffIdx; i++ {
			path += legs[i].DistanceMeters
		}
	}
	return etas, clock, path, true
}

func nearestCandidates(eligible []model.SupplyView, work map[string][]model.Stop, pickup model.Location, seats int, current string, limit int) []model.SupplyView {
	type ranked struct {
		supply   model.SupplyView
		distance int64
	}
	var open, busy []ranked
	for _, s := range eligible {
		if s.SupplyID == current || s.NumAvailableSeats < seats {
			continue
		}
		entry := ranked{s, routing.HaversineMeters(s.Location, pickup)}
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
		if !model.SameStop(a[i], b[i]) {
			return false
		}
	}
	return true
}
