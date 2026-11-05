package simulator

import "github.com/realjonathanhester/ride-hailing-simulator/model"

// tripLeg is one vehicle leg of a (possibly multi-leg) trip.
type tripLeg struct {
	supplyID        string
	start           model.Location
	end             model.Location
	pickupAt        *int64
	dropoffAt       *int64
	distanceMeters  int64
	durationSeconds int64
}

func (s *Simulator) stopLocation(r *request, stop model.Stop) model.Location {
	if stop.Location != nil {
		return *stop.Location
	}
	if stop.Type == model.Pickup {
		return r.input.Origin
	}
	return r.input.Destination
}

func (s *Simulator) isFinalDropoff(r *request, stop model.Stop) bool {
	return stop.Type == model.Dropoff && s.stopLocation(r, stop) == r.input.Destination
}

func (s *Simulator) isHandoffDropoff(r *request, stop model.Stop) bool {
	return stop.Type == model.Dropoff && !s.isFinalDropoff(r, stop)
}

func defaultLocation(r *request, t model.StopType) model.Location {
	if t == model.Pickup {
		return r.input.Origin
	}
	return r.input.Destination
}

// plansInvolveHandoff reports whether any proposed stop uses handoff
// semantics, so the batch must apply all-or-nothing.
func (s *Simulator) plansInvolveHandoff(proposed map[string][]model.Stop) bool {
	if !s.settings.HandoffsEnabled {
		return false
	}
	for _, plan := range proposed {
		for _, stop := range plan {
			r := s.requests[stop.RequestID]
			if r == nil {
				continue
			}
			if stop.Location != nil && *stop.Location != defaultLocation(r, stop.Type) {
				return true
			}
			if stop.Type == model.Pickup && r.status == model.StatusWaitingForHandoff {
				return true
			}
			if stop.Type == model.Pickup && r.handoffLocation != nil && r.status != model.StatusOnTrip {
				return true
			}
		}
	}
	return false
}

// validateHandoffBatch checks post-batch handoff rules. proposed entries that
// are in rejected are ignored (those cars keep their current plans).
//
// v1 installs onward pickups only after the handoff dropoff (via a later
// matcher call). A handoff dropoff may therefore have zero onward pickups in
// the same batch; if any are present there must be exactly one at the handoff
// location. At most one pickup globally per request is also enforced in
// applyChanges.
func (s *Simulator) validateHandoffBatch(proposed map[string][]model.Stop, rejected map[string]bool) bool {
	postPlan := func(id string) []model.Stop {
		if plan, ok := proposed[id]; ok && !rejected[id] {
			return plan
		}
		out := make([]model.Stop, 0, len(s.cars[id].stops))
		for _, stop := range s.cars[id].stops {
			out = append(out, stop.Stop)
		}
		return out
	}

	type ref struct {
		supplyID string
		stop     model.Stop
	}
	pickups := map[string][]ref{}
	finalDropoffs := map[string][]ref{}
	handoffDropoffs := map[string][]ref{}

	for _, id := range s.carOrder {
		c := s.cars[id]
		if !c.online || c.offline {
			continue
		}
		for _, stop := range postPlan(id) {
			r := s.requests[stop.RequestID]
			if r == nil {
				return false
			}
			switch stop.Type {
			case model.Pickup:
				pickups[stop.RequestID] = append(pickups[stop.RequestID], ref{id, stop})
			case model.Dropoff:
				if s.isHandoffDropoff(r, stop) {
					handoffDropoffs[stop.RequestID] = append(handoffDropoffs[stop.RequestID], ref{id, stop})
				} else {
					finalDropoffs[stop.RequestID] = append(finalDropoffs[stop.RequestID], ref{id, stop})
				}
			}
		}
	}

	for requestID, drops := range handoffDropoffs {
		if len(drops) != 1 {
			return false
		}
		r := s.requests[requestID]
		handoffLoc := s.stopLocation(r, drops[0].stop)
		if handoffLoc == r.input.Destination {
			return false
		}
		onward := 0
		for _, p := range pickups[requestID] {
			if s.stopLocation(r, p.stop) == handoffLoc {
				onward++
			}
		}
		// Zero onward is allowed (assigned on a later match after dropoff);
		// more than one is never allowed.
		if onward > 1 {
			return false
		}
	}

	for _, id := range s.requestOrder {
		r := s.requests[id]
		if r.handoffLocation == nil {
			continue
		}
		if r.status != model.StatusWaitingForHandoff && r.status != model.StatusAssigned && r.status != model.StatusDriverArrived {
			continue
		}
		onward := 0
		for _, p := range pickups[id] {
			if s.stopLocation(r, p.stop) != *r.handoffLocation {
				return false
			}
			onward++
		}
		if onward > 1 {
			return false
		}
	}

	// At most one pickup per request in the post-batch state (malformed
	// handoffs included). The first-leg + single onward pair is allowed only
	// when a handoff dropoff is present for that request.
	for requestID, refs := range pickups {
		if len(refs) <= 1 {
			continue
		}
		r := s.requests[requestID]
		if r == nil || len(refs) != 2 || len(handoffDropoffs[requestID]) != 1 {
			return false
		}
		handoffLoc := s.stopLocation(r, handoffDropoffs[requestID][0].stop)
		first, onward := 0, 0
		for _, p := range refs {
			loc := s.stopLocation(r, p.stop)
			if loc == r.input.Origin {
				first++
			} else if loc == handoffLoc {
				onward++
			}
		}
		if first != 1 || onward != 1 {
			return false
		}
	}

	for _, drops := range finalDropoffs {
		if len(drops) > 1 {
			return false
		}
	}
	return true
}

// dropOnwardPickupsForOffline removes onward pickups (and their non-aboard
// dropoffs) for waiting or onward-assigned riders from a car that is going
// offline, leaves each rider waiting_for_handoff and unassigned with cleared
// assignment fields, and replans the car. Never removes an aboard rider's
// dropoff. The handoff timeout guarantees eventual resolution.
func (s *Simulator) dropOnwardPickupsForOffline(c *car) {
	if !s.settings.HandoffsEnabled {
		return
	}
	dropPickup := map[string]bool{}
	for _, stop := range c.stops {
		r := s.requests[stop.RequestID]
		onwardPickup := stop.Type == model.Pickup && r.handoffLocation != nil &&
			(r.status == model.StatusWaitingForHandoff || r.status == model.StatusAssigned || r.status == model.StatusDriverArrived) &&
			r.supplyID == c.input.SupplyID && c.aboard[stop.RequestID] == 0
		if onwardPickup {
			dropPickup[stop.RequestID] = true
		}
	}
	if len(dropPickup) == 0 {
		return
	}
	var remaining []model.Stop
	var dropped []string
	seenDropped := map[string]bool{}
	for _, stop := range c.stops {
		if dropPickup[stop.RequestID] {
			// Keep an aboard rider's dropoff (should not apply for onward
			// pickups, but guard pooled plans).
			if stop.Type == model.Dropoff && c.aboard[stop.RequestID] > 0 {
				remaining = append(remaining, stop.Stop)
				continue
			}
			if !seenDropped[stop.RequestID] {
				dropped = append(dropped, stop.RequestID)
				seenDropped[stop.RequestID] = true
			}
			continue
		}
		remaining = append(remaining, stop.Stop)
	}
	for _, id := range dropped {
		r := s.requests[id]
		r.status = model.StatusWaitingForHandoff
		r.supplyID = ""
		r.assignedAt, r.etaAtAssignment = nil, nil
	}
	planned, err := s.replan(c, remaining)
	for _, id := range dropped {
		s.log(c, "stop_removed", id, nil)
	}
	if err != nil {
		return
	}
	if len(remaining) == 0 {
		if c.goingOffline {
			s.goOffline(c)
		} else {
			s.becameEmpty(c)
		}
	} else {
		s.log(c, "plan_updated", "", planned)
	}
}
