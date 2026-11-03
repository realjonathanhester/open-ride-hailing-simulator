package simulator

import (
	"time"

	"github.com/realjonathanhester/open-ride-hailing-simulator/model"
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
		if !r.arrived || r.status != model.StatusRequested && r.status != model.StatusAssigned && r.status != model.StatusDriverArrived {
			continue
		}
		view := model.RequestView{RequestID: id, RequestedAt: r.input.RequestedAt, Origin: r.input.Origin, Destination: r.input.Destination,
			NumRequestedSeats: r.seats, Status: r.status, SupplyID: r.supplyID, AssignedAt: r.assignedAt, CancelAt: r.cancelAt()}
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
	return earliest
}

func sameStops(c *car, plan []model.Stop) bool {
	if len(c.stops) != len(plan) {
		return false
	}
	for i := range plan {
		if c.stops[i].Stop != plan[i] {
			return false
		}
	}
	return true
}

// validChange checks the rules that apply to one plan on its own.
func (s *Simulator) validChange(c *car, plan []model.Stop) bool {
	if !c.online || c.offline {
		return false
	}
	current := map[model.Stop]bool{}
	for _, stop := range c.stops {
		current[stop.Stop] = true
	}
	if c.phase == phaseDwelling && (len(plan) == 0 || plan[0] != c.stops[0].Stop) {
		return false
	}
	seen := map[model.Stop]bool{}
	seats, riders := c.seatsInUse, len(c.aboard)
	for _, stop := range plan {
		r, ok := s.requests[stop.RequestID]
		if !ok || seen[stop] || (stop.Type != model.Pickup && stop.Type != model.Dropoff) {
			return false
		}
		seen[stop] = true
		_, aboard := c.aboard[stop.RequestID]
		switch stop.Type {
		case model.Pickup:
			switch r.status {
			case model.StatusRequested:
			case model.StatusAssigned:
				if r.supplyID != c.input.SupplyID && !s.settings.ReassignmentEnabled {
					return false
				}
			case model.StatusDriverArrived:
				if r.supplyID != c.input.SupplyID {
					return false
				}
			default:
				return false
			}
			if c.goingOffline && !current[stop] {
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
		if stop.Type != model.Pickup || seen[stop.Stop] {
			continue
		}
		r := s.requests[stop.RequestID]
		if !s.settings.ReassignmentEnabled || r.status == model.StatusDriverArrived {
			return false // can't drop an assigned request, or one whose car has arrived
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
		if sameStops(s.cars[id], plan) {
			delete(proposed, id)
		} else if !s.validChange(s.cars[id], plan) {
			rejected[id] = true
		}
	}
	// A request may be in at most one plan; reject every change that clashes.
	for changed := true; changed; {
		changed = false
		owners := map[string][]string{}
		for _, id := range s.carOrder {
			plan, ok := proposed[id]
			if !ok || rejected[id] {
				for _, stop := range s.cars[id].stops {
					plan = append(plan, stop.Stop)
				}
			}
			for _, stop := range plan {
				if stop.Type == model.Pickup || (stop.Type == model.Dropoff && s.cars[id].aboard[stop.RequestID] > 0) {
					owners[stop.RequestID] = append(owners[stop.RequestID], id)
				}
			}
		}
		for _, ids := range owners {
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
		for _, stop := range c.stops {
			if stop.Type == model.Pickup {
				previousOwners[stop.RequestID] = id
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
			} else if r.previousSupplyID != c.input.SupplyID {
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
		r.status, r.supplyID = model.StatusRequested, ""
		if r.assignmentDeadline != nil && s.now >= *r.assignmentDeadline {
			s.cancel(r, "max_request_to_assignment")
		}
	}
}
