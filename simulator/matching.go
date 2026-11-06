package simulator

import (
	"time"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
)

func (s *Simulator) match(trigger string) {
	if s.now < s.startAt {
		return // the clock starts at the first requested_at
	}
	s.matcherCalls[trigger]++
	input := model.MatchInput{Now: s.now, Trigger: trigger, Settings: s.settings, Router: s.router}
	for _, id := range s.carOrder {
		c := s.cars[id]
		if !c.online || c.offline {
			continue
		}
		location, _ := s.position(c)
		view := model.SupplyView{SupplyID: id, Location: location, State: c.state(), NumAvailableSeats: c.capacity, SeatsInUse: c.seatsInUse}
		for _, stop := range c.stops {
			view.Plan = append(view.Plan, model.Waypoint{RequestID: stop.RequestID, Type: stop.Type, Location: stop.location, EtaAt: stop.etaAt, NumRequestedSeats: stop.seats})
		}
		input.Supplies = append(input.Supplies, view)
	}
	for _, id := range s.requestOrder {
		r := s.requests[id]
		if !r.arrived {
			continue
		}
		switch r.status {
		case model.StatusRequested, model.StatusAssigned, model.StatusDriverArrived, model.StatusWaitingForHandoff:
		default:
			continue
		}
		view := model.RequestView{RequestID: id, RequestedAt: r.input.RequestedAt, Origin: r.input.Origin, Destination: r.input.Destination,
			NumRequestedSeats: r.seats, Status: r.status, SupplyID: r.supplyID, AssignedAt: r.assignedAt, CancelAt: r.cancelAt(),
			HandoffLocation: r.handoffLocation, RealizedAboardDistanceMeters: realizedAboardDistance(r)}
		input.Requests = append(input.Requests, view)
	}
	began := time.Now()
	changes := s.matcher.Match(input)
	s.matcherElapsed += time.Since(began)
	s.applyChanges(changes)
}

func (r *request) cancelAt() *int64 {
	var earliest *int64
	if r.status == model.StatusRequested {
		earliest = r.assignmentDeadline
	}
	if r.pickupDeadline != nil && (earliest == nil || *r.pickupDeadline < *earliest) {
		earliest = r.pickupDeadline
	}
	if r.status == model.StatusWaitingForHandoff || (r.handoffLocation != nil && (r.status == model.StatusAssigned || r.status == model.StatusDriverArrived)) {
		if r.handoffDeadline != nil && (earliest == nil || *r.handoffDeadline < *earliest) {
			earliest = r.handoffDeadline
		}
	}
	return earliest
}

func (s *Simulator) sameStops(c *car, plan []model.Stop) bool {
	if len(c.stops) != len(plan) {
		return false
	}
	for i := range plan {
		if model.StopKey(c.stops[i].Stop) != model.StopKey(plan[i]) {
			return false
		}
		r := s.requests[plan[i].RequestID]
		if r == nil || c.stops[i].location != s.stopLocation(r, plan[i]) {
			return false
		}
	}
	return true
}

func currentStopKeys(c *car) map[model.Stop]bool {
	current := map[model.Stop]bool{}
	for _, stop := range c.stops {
		current[model.StopKey(stop.Stop)] = true
	}
	return current
}

// validChange checks the rules that apply to one plan on its own.
func (s *Simulator) validChange(c *car, plan []model.Stop) bool {
	if !c.online || c.offline {
		return false
	}
	current := currentStopKeys(c)
	if c.phase == phaseDwelling {
		if len(c.stops) == 0 || len(plan) == 0 || model.StopKey(plan[0]) != model.StopKey(c.stops[0].Stop) {
			return false
		}
	}
	seen := map[model.Stop]bool{}
	seats, riders := c.seatsInUse, len(c.aboard)
	for _, stop := range plan {
		r, ok := s.requests[stop.RequestID]
		key := model.StopKey(stop)
		if !ok || seen[key] || (stop.Type != model.Pickup && stop.Type != model.Dropoff) {
			return false
		}
		seen[key] = true
		_, aboard := c.aboard[stop.RequestID]
		switch stop.Type {
		case model.Pickup:
			if aboard {
				return false
			}
			switch r.status {
			case model.StatusRequested:
			case model.StatusWaitingForHandoff:
				if !s.settings.HandoffsEnabled {
					return false
				}
				if r.handoffLocation == nil || stop.Location == nil || *stop.Location != *r.handoffLocation {
					return false
				}
			case model.StatusAssigned:
				if r.supplyID != c.input.SupplyID {
					if s.settings.HandoffsEnabled && stop.Location != nil && *stop.Location != r.input.Origin {
						// Linked onward pickup; batch validation confirms the handoff.
					} else if r.handoffLocation != nil && stop.Location != nil && *stop.Location == *r.handoffLocation {
						// Rematch after an onward car dropped the rider.
					} else if !s.settings.ReassignmentEnabled {
						return false
					}
				} else if r.handoffLocation != nil {
					if stop.Location == nil || *stop.Location != *r.handoffLocation {
						return false
					}
				}
			case model.StatusDriverArrived:
				if r.supplyID != c.input.SupplyID {
					return false
				}
			case model.StatusOnTrip:
				// Onward pickups are assigned only after the handoff dropoff makes the
				// rider waiting_for_handoff; never board a rider still on another car.
				return false
			default:
				return false
			}
			if c.goingOffline && !current[key] {
				return false
			}
			seats += r.seats
			riders++
		case model.Dropoff:
			if aboard {
				// fine: rider aboard this car
			} else if !seen[model.Stop{RequestID: stop.RequestID, Type: model.Pickup}] {
				return false
			}
			if s.isHandoffDropoff(r, stop) {
				if !s.settings.HandoffsEnabled {
					return false
				}
				if stop.Location == nil || *stop.Location == r.input.Destination {
					return false
				}
			}
			seats -= r.seats
			riders--
		}
		if seats > c.capacity || (!s.settings.PoolingEnabled && riders > 1) {
			return false
		}
	}
	for id := range c.aboard {
		if !seen[model.Stop{RequestID: id, Type: model.Dropoff}] {
			return false
		}
	}
	for stop := range seen {
		if stop.Type == model.Pickup && !seen[model.Stop{RequestID: stop.RequestID, Type: model.Dropoff}] {
			return false
		}
	}
	for _, stop := range c.stops {
		if stop.Type != model.Pickup || seen[model.StopKey(stop.Stop)] {
			continue
		}
		r := s.requests[stop.RequestID]
		if r.status == model.StatusDriverArrived {
			return false // can't drop a request whose car has arrived
		}
		// Handoff onward pickups may be moved only with reassignment; the apply
		// path must emit a visible unassignment (see reconcileAssignments).
		if r.handoffLocation != nil || (stop.Location != nil && *stop.Location != r.input.Origin) {
			if !s.settings.ReassignmentEnabled {
				return false
			}
			continue
		}
		if !s.settings.ReassignmentEnabled {
			return false // can't drop an assigned request
		}
	}
	return true
}

func (s *Simulator) applyChanges(changes []model.PlanChange) {
	proposed := map[string][]model.Stop{}
	rejected := map[string]bool{}
	for _, change := range changes {
		if _, ok := s.cars[change.SupplyID]; !ok {
			continue
		}
		if _, duplicate := proposed[change.SupplyID]; duplicate {
			rejected[change.SupplyID] = true
		}
		proposed[change.SupplyID] = change.Plan
	}
	for id, plan := range proposed {
		if rejected[id] {
			continue
		}
		if s.sameStops(s.cars[id], plan) {
			delete(proposed, id)
		} else if !s.validChange(s.cars[id], plan) {
			rejected[id] = true
		}
	}

	handoffBatch := s.plansInvolveHandoff(proposed)

	// At most one pickup globally per request. A first-leg pickup (origin) plus
	// one onward pickup at a handoff dropoff location is the only allowed pair.
	for changed := true; changed; {
		changed = false
		pickupOwners := map[string][]struct {
			supplyID string
			stop     model.Stop
		}{}
		aboardDropoffOwners := map[string][]string{}
		handoffDropLoc := map[string]model.Location{}
		for _, id := range s.carOrder {
			plan, ok := proposed[id]
			if !ok || rejected[id] {
				plan = nil
				for _, stop := range s.cars[id].stops {
					plan = append(plan, stop.Stop)
				}
			}
			for _, stop := range plan {
				r := s.requests[stop.RequestID]
				if r == nil {
					continue
				}
				if stop.Type == model.Pickup {
					pickupOwners[stop.RequestID] = append(pickupOwners[stop.RequestID], struct {
						supplyID string
						stop     model.Stop
					}{id, stop})
				}
				if stop.Type == model.Dropoff && s.cars[id].aboard[stop.RequestID] > 0 {
					aboardDropoffOwners[stop.RequestID] = append(aboardDropoffOwners[stop.RequestID], id)
				}
				if r != nil && s.isHandoffDropoff(r, stop) {
					handoffDropLoc[stop.RequestID] = s.stopLocation(r, stop)
				}
			}
		}
		for requestID, pickups := range pickupOwners {
			r := s.requests[requestID]
			allowed := len(pickups) <= 1
			if !allowed && len(pickups) == 2 && r != nil {
				handoffLoc, hasHandoff := handoffDropLoc[requestID]
				if hasHandoff {
					first, onward := 0, 0
					for _, p := range pickups {
						loc := s.stopLocation(r, p.stop)
						if loc == r.input.Origin {
							first++
						} else if loc == handoffLoc {
							onward++
						}
					}
					allowed = first == 1 && onward == 1
				}
			}
			if allowed {
				continue
			}
			for _, p := range pickups {
				if _, ok := proposed[p.supplyID]; ok && !rejected[p.supplyID] {
					rejected[p.supplyID], changed = true, true
				}
			}
		}
		// Aboard dropoffs on two cars still clash.
		for _, ids := range aboardDropoffOwners {
			if len(ids) < 2 {
				continue
			}
			for _, id := range ids {
				if _, ok := proposed[id]; ok && !rejected[id] {
					rejected[id], changed = true, true
				}
			}
		}
	}

	if handoffBatch {
		if !s.validateHandoffBatch(proposed, rejected) {
			for id := range proposed {
				rejected[id] = true
			}
		}
		// Any single-car rejection fails the whole linked batch.
		for id := range proposed {
			if rejected[id] {
				for other := range proposed {
					rejected[other] = true
				}
				break
			}
		}
		allRejected := len(proposed) > 0
		for id := range proposed {
			if !rejected[id] {
				allRejected = false
				break
			}
		}
		if allRejected {
			for _, id := range s.carOrder {
				if _, ok := proposed[id]; ok {
					s.sync(s.cars[id])
					s.log(s.cars[id], "plan_rejected", "", nil)
				}
			}
			return
		}
	}

	// Pre-check driver declines and routes for handoff batches so a failure
	// rolls back every plan change.
	type prepared struct {
		car     *car
		plan    []model.Stop
		planned []model.Location
	}
	var ready []prepared
	if handoffBatch {
		declinedOrFailed := false
		for _, id := range s.carOrder {
			plan, ok := proposed[id]
			if !ok || rejected[id] {
				continue
			}
			c := s.cars[id]
			s.sync(c)
			if addsPickup(c, plan) && !s.behavior.OnAssignmentOffered(s.driverState(c), plan) {
				declinedOrFailed = true
				break
			}
			planned, err := s.routePlan(c, plan)
			if err != nil {
				declinedOrFailed = true
				break
			}
			ready = append(ready, prepared{c, plan, planned})
		}
		if declinedOrFailed {
			for _, id := range s.carOrder {
				if _, ok := proposed[id]; ok {
					s.sync(s.cars[id])
					s.log(s.cars[id], "plan_rejected", "", nil)
				}
			}
			return
		}
		previousOwners := map[string]string{}
		var accepted []*car
		for _, p := range ready {
			for _, stop := range p.car.stops {
				if stop.Type == model.Pickup {
					previousOwners[stop.RequestID] = p.car.input.SupplyID
				}
			}
			// Log visible unassignment when a handoff onward pickup is dropped.
			for _, stop := range p.car.stops {
				if stop.Type != model.Pickup {
					continue
				}
				still := false
				for _, n := range p.plan {
					if model.StopKey(n) == model.StopKey(stop.Stop) {
						still = true
						break
					}
				}
				if still {
					continue
				}
				r := s.requests[stop.RequestID]
				if r != nil && r.handoffLocation != nil {
					s.log(p.car, "stop_removed", stop.RequestID, nil)
				}
			}
			s.installPlan(p.car, p.plan, p.planned)
			accepted = append(accepted, p.car)
		}
		s.reconcileAssignments(accepted, previousOwners)
		return
	}

	previousOwners := map[string]string{}
	var accepted []*car
	for _, id := range s.carOrder {
		plan, ok := proposed[id]
		if !ok {
			continue
		}
		c := s.cars[id]
		s.sync(c)
		if rejected[id] {
			s.log(c, "plan_rejected", "", nil)
			continue
		}
		if addsPickup(c, plan) && !s.behavior.OnAssignmentOffered(s.driverState(c), plan) {
			s.log(c, "plan_declined", "", nil)
			continue
		}
		for _, stop := range c.stops {
			if stop.Type == model.Pickup {
				previousOwners[stop.RequestID] = id
			}
		}
		for _, stop := range c.stops {
			if stop.Type != model.Pickup {
				continue
			}
			still := false
			for _, n := range plan {
				if model.StopKey(n) == model.StopKey(stop.Stop) {
					still = true
					break
				}
			}
			if still {
				continue
			}
			r := s.requests[stop.RequestID]
			if r != nil && r.handoffLocation != nil {
				s.log(c, "stop_removed", stop.RequestID, nil)
			}
		}
		planned, err := s.replan(c, plan)
		if err != nil {
			s.log(c, "plan_rejected", "", nil)
			continue
		}
		s.log(c, "plan_updated", "", planned)
		if len(plan) == 0 {
			s.becameEmpty(c)
		}
		accepted = append(accepted, c)
	}
	s.reconcileAssignments(accepted, previousOwners)
}

// routePlan computes the routed waypoints for a plan without installing it.
func (s *Simulator) routePlan(c *car, stops []model.Stop) ([]model.Location, error) {
	newStops := make([]planStop, len(stops))
	for i, stop := range stops {
		r := s.requests[stop.RequestID]
		newStops[i] = planStop{Stop: stop, location: s.stopLocation(r, stop), seats: r.seats}
	}
	start := c.baseLocation
	routed := newStops
	if c.phase == phaseDwelling {
		newStops[0] = c.stops[0]
		newStops[0].Stop = stops[0] // allow location update on the dwelling stop identity
		newStops[0].location = s.stopLocation(s.requests[stops[0].RequestID], stops[0])
		start, routed = newStops[0].location, newStops[1:]
	}
	waypoints := make([]model.Location, len(routed))
	for i, stop := range routed {
		waypoints[i] = stop.location
	}
	legs, err := s.route(start, waypoints)
	if err != nil {
		return nil, err
	}
	planned, _ := plannedGeometry(start, waypoints, legs)
	return planned, nil
}

// installPlan applies a already-validated, already-routable plan to a synced car.
func (s *Simulator) installPlan(c *car, plan []model.Stop, planned []model.Location) {
	if _, err := s.replan(c, plan); err != nil {
		// routePlan succeeded; replan must too from the same state.
		s.log(c, "plan_rejected", "", nil)
		return
	}
	s.log(c, "plan_updated", "", planned)
	if len(plan) == 0 {
		s.becameEmpty(c)
	}
}

func (s *Simulator) reconcileAssignments(accepted []*car, previousOwners map[string]string) {
	owner := map[string]string{}
	for _, c := range accepted {
		for _, stop := range c.stops {
			if stop.Type != model.Pickup {
				continue
			}
			owner[stop.RequestID] = c.input.SupplyID
			r := s.requests[stop.RequestID]
			if r.supplyID == c.input.SupplyID {
				continue
			}
			at := s.now
			if r.firstAssignedAt == nil {
				r.firstAssignedAt = &at
			} else if r.previousSupplyID != "" && r.previousSupplyID != c.input.SupplyID && r.handoffLocation == nil {
				r.reassignCount++
			}
			eta := stop.etaAt - s.now
			r.assignedAt, r.supplyID, r.previousSupplyID, r.status, r.etaAtAssignment = &at, c.input.SupplyID, c.input.SupplyID, model.StatusAssigned, &eta
			r.actionIndexAtAssign, r.odometerAtAssign = len(c.actions), c.baseOdometer
		}
	}
	for _, id := range s.requestOrder {
		r := s.requests[id]
		if previousOwners[id] == "" || owner[id] != "" || r.supplyID != previousOwners[id] {
			continue
		}
		if r.handoffLocation != nil {
			r.status, r.supplyID = model.StatusWaitingForHandoff, ""
			r.assignedAt, r.etaAtAssignment = nil, nil
			continue
		}
		r.status, r.supplyID = model.StatusRequested, ""
		if r.assignmentDeadline != nil && s.now >= *r.assignmentDeadline {
			s.cancel(r, "max_request_to_assignment")
		}
	}
}

// addsPickup reports whether a plan has a pickup the car doesn't already have.
func addsPickup(c *car, plan []model.Stop) bool {
	current := currentStopKeys(c)
	for _, stop := range plan {
		if stop.Type == model.Pickup && !current[model.StopKey(stop)] {
			return true
		}
	}
	return false
}

// realizedAboardDistance sums completed trip-leg distances for the matcher.
func realizedAboardDistance(r *request) int64 {
	var total int64
	for _, leg := range r.legs {
		if leg.dropoffAt == nil {
			continue // in-progress leg has no final distance yet
		}
		total += leg.distanceMeters
	}
	return total
}
