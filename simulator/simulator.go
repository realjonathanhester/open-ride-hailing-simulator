// Package simulator is the deterministic discrete-event core.
package simulator

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/realjonathanhester/ride-hailing-simulator/driverbehavior"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
)

const (
	phaseNone = iota
	phaseDriving
	phaseDwelling
	phaseRepositioning // empty plan, driving to repositionTarget
)

// Activity classes used for the supply summary.
const (
	classIdle = iota
	classEnRoute
	classOnTrip
)

type planStop struct {
	model.Stop
	location    model.Location
	seats       int
	etaAt       int64
	legDistance int64 // from the previous stop (or the plan start)
	legDuration int64
}

type request struct {
	arrived               bool
	input                 model.Request
	seats                 int
	status                string
	supplyID              string
	previousSupplyID      string
	firstAssignedAt       *int64
	assignedAt            *int64
	reassignCount         int
	etaAtAssignment       *int64
	driverArrivedAt       *int64
	pickupAt              *int64
	dropoffAt             *int64
	canceledAt            *int64
	cancelReason          string
	actionIndexAtAssign   int
	odometerAtAssign      int64
	odometerAtPickup      int64
	odometerAtDropoff     int64
	directDistanceMeters  int64
	directDurationSeconds int64
	maxCoRiders           int
	ridepoolType          string
	assignmentDeadline    *int64
	pickupDeadline        *int64
}

type car struct {
	input        model.Supply
	capacity     int
	online       bool
	offline      bool
	goingOffline bool
	offlineAt    *int64

	baseLocation model.Location
	baseAt       int64
	baseOdometer int64
	phase        int
	legRemaining int64 // meters from base to stops[0] while driving
	legEndAt     int64
	dwellEndAt   int64
	// repositionTarget is where the car drives while phaseRepositioning.
	repositionTarget model.Location
	stops            []planStop
	version          int64

	aboard     map[string]int
	seatsInUse int

	planID    string
	planCount int

	lastRowLocation model.Location
	path            []model.Location
	actions         []model.Stop

	accountedAt  int64
	classSeconds [3]int64
	classMeters  [3]int64
	served       int
}

// Simulator runs one simulation.
type Simulator struct {
	settings model.Settings
	router   model.Router
	matcher  model.Matcher
	behavior model.DriverBehavior

	requests     map[string]*request
	requestOrder []string
	cars         map[string]*car
	carOrder     []string

	queue    eventQueue
	sequence int64
	now      int64
	startAt  int64
	endAt    int64

	eventRows      [][]string
	matcherCalls   map[string]int
	matcherElapsed time.Duration
	routerElapsed  time.Duration
}

// New creates a simulator with the given hooks.
func New(settings model.Settings, router model.Router, matcher model.Matcher) *Simulator {
	return &Simulator{settings: settings, router: router, matcher: matcher, behavior: driverbehavior.Simple{},
		requests: map[string]*request{}, cars: map[string]*car{},
		matcherCalls: map[string]int{model.TriggerRequest: 0, model.TriggerSupplyOnline: 0, model.TriggerSupplyOffline: 0, model.TriggerBatchTick: 0}}
}

// SetDriverBehavior replaces the default simple driver behavior.
func (s *Simulator) SetDriverBehavior(behavior model.DriverBehavior) {
	s.behavior = behavior
}

func validate(settings model.Settings, requests []model.Request, supplies []model.Supply) error {
	seen := map[string]bool{}
	for _, r := range requests {
		if r.RequestID == "" || seen[r.RequestID] {
			return fmt.Errorf("request_id %q is empty or not unique", r.RequestID)
		}
		seen[r.RequestID] = true
		if r.NumRequestedSeats != nil && *r.NumRequestedSeats < 1 {
			return fmt.Errorf("request %s: num_requested_seats must be at least 1", r.RequestID)
		}
	}
	seen = map[string]bool{}
	for _, s := range supplies {
		if s.SupplyID == "" || seen[s.SupplyID] {
			return fmt.Errorf("supply_id %q is empty or not unique", s.SupplyID)
		}
		seen[s.SupplyID] = true
		if s.NumAvailableSeats != nil && *s.NumAvailableSeats < 1 {
			return fmt.Errorf("supply %s: num_available_seats must be at least 1", s.SupplyID)
		}
		if s.OfflineAt != nil && *s.OfflineAt < s.OnlineAt {
			return fmt.Errorf("supply %s: offline_at is before online_at", s.SupplyID)
		}
	}
	if settings.DefaultNumRequestedSeats < 1 || settings.DefaultNumAvailableSeats < 1 {
		return fmt.Errorf("default seat counts must be at least 1")
	}
	if settings.BatchMatchingIntervalSeconds != nil && *settings.BatchMatchingIntervalSeconds <= 0 {
		return fmt.Errorf("batch_matching_interval_seconds must be positive")
	}
	return nil
}

func (s *Simulator) route(start model.Location, waypoints []model.Location) ([]model.Leg, error) {
	began := time.Now()
	legs, err := s.router.Route(start, waypoints, s.now)
	s.routerElapsed += time.Since(began)
	if err == nil && len(legs) != len(waypoints) {
		err = fmt.Errorf("router returned %d legs for %d waypoints", len(legs), len(waypoints))
	}
	return legs, err
}

// Run executes the simulation and returns every output.
func (s *Simulator) Run(requests []model.Request, supplies []model.Supply) (*Result, error) {
	if err := validate(s.settings, requests, supplies); err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("no ride requests")
	}
	s.startAt = math.MaxInt64
	for _, in := range requests {
		r := &request{input: in, seats: s.settings.DefaultNumRequestedSeats, status: model.StatusRequested}
		if in.NumRequestedSeats != nil {
			r.seats = *in.NumRequestedSeats
		}
		if in.RequestedAt < s.startAt {
			s.startAt = in.RequestedAt
		}
		s.now = in.RequestedAt
		legs, err := s.route(in.Origin, []model.Location{in.Destination})
		if err != nil {
			return nil, fmt.Errorf("request %s: %w", in.RequestID, err)
		}
		r.directDistanceMeters, r.directDurationSeconds = legs[0].DistanceMeters, legs[0].DurationSeconds
		s.requests[in.RequestID] = r
		s.requestOrder = append(s.requestOrder, in.RequestID)
		s.push(&event{at: in.RequestedAt, priority: priorityRequest, id: in.RequestID, kind: eventRequest})
	}
	sort.Strings(s.requestOrder)
	for _, in := range supplies {
		c := &car{input: in, capacity: s.settings.DefaultNumAvailableSeats, aboard: map[string]int{}}
		if in.NumAvailableSeats != nil {
			c.capacity = *in.NumAvailableSeats
		}
		s.cars[in.SupplyID] = c
		s.carOrder = append(s.carOrder, in.SupplyID)
		s.push(&event{at: in.OnlineAt, priority: prioritySupplyOnline, id: in.SupplyID, kind: eventSupplyOnline})
		if in.OfflineAt != nil {
			s.push(&event{at: *in.OfflineAt, priority: priorityOffline, id: in.SupplyID, kind: eventSupplyOffline})
		}
	}
	sort.Strings(s.carOrder)
	if interval := s.settings.BatchMatchingIntervalSeconds; interval != nil {
		s.push(&event{at: s.startAt + *interval, priority: priorityBatchTick, id: "batch_tick", kind: eventBatchTick})
	}
	s.endAt = s.startAt

	for s.queue.Len() > 0 {
		e := heap.Pop(&s.queue).(*event)
		if e.kind == eventBatchTick {
			if !s.hasLiveWork() {
				break
			}
		} else if !s.isLive(e) {
			continue
		}
		s.now = e.at
		if e.kind != eventBatchTick && e.at > s.endAt {
			s.endAt = e.at
		}
		s.apply(e)
	}
	for _, id := range s.carOrder {
		if c := s.cars[id]; c.online && !c.offline {
			s.now = s.endAt
			s.sync(c)
		}
	}
	result := s.result()
	result.RunMetadata = s.runMetadata(requests, supplies)
	return result, nil
}

func (s *Simulator) isLive(e *event) bool {
	switch e.kind {
	case eventArrive, eventDwellDone:
		return s.cars[e.id].version == e.version
	case eventAssignmentDeadline:
		return s.requests[e.id].status == model.StatusRequested || s.requests[e.id].status == model.StatusAssigned || s.requests[e.id].status == model.StatusDriverArrived
	case eventPickupDeadline:
		st := s.requests[e.id].status
		return st == model.StatusRequested || st == model.StatusAssigned || st == model.StatusDriverArrived
	case eventSupplyOffline:
		return !s.cars[e.id].offline
	}
	return true
}

func (s *Simulator) hasLiveWork() bool {
	for _, e := range s.queue {
		if e.kind != eventBatchTick && s.isLive(e) {
			return true
		}
	}
	return false
}

func (s *Simulator) apply(e *event) {
	switch e.kind {
	case eventRequest:
		r := s.requests[e.id]
		r.arrived = true
		if limit := pick(r.input.MaxRequestToAssignmentSeconds, s.settings.MaxRequestToAssignmentSeconds); limit != nil {
			at := r.input.RequestedAt + *limit
			r.assignmentDeadline = &at
			s.push(&event{at: at, priority: priorityCancellation, id: r.input.RequestID, kind: eventAssignmentDeadline})
		}
		if limit := pick(r.input.MaxRequestToPickupSeconds, s.settings.MaxRequestToPickupSeconds); limit != nil {
			at := r.input.RequestedAt + *limit
			r.pickupDeadline = &at
			s.push(&event{at: at, priority: priorityCancellation, id: r.input.RequestID, kind: eventPickupDeadline})
		}
		s.match(model.TriggerRequest)
	case eventSupplyOnline:
		c := s.cars[e.id]
		c.online = true
		c.baseLocation, c.baseAt, c.accountedAt = c.input.OnlineLocation, s.now, s.now
		c.lastRowLocation = c.baseLocation
		c.path = []model.Location{c.baseLocation}
		s.log(c, "online", "", nil)
		s.match(model.TriggerSupplyOnline)
	case eventSupplyOffline:
		c := s.cars[e.id]
		if !c.online {
			c.offline = true // never came online
			return
		}
		if len(c.stops) == 0 {
			s.goOffline(c)
		} else {
			s.sync(c)
			c.goingOffline = true
		}
		s.match(model.TriggerSupplyOffline)
	case eventArrive:
		if c := s.cars[e.id]; c.phase == phaseRepositioning {
			s.arriveReposition(c)
		} else {
			s.arrive(c)
		}
	case eventDwellDone:
		s.finishStop(s.cars[e.id])
	case eventAssignmentDeadline:
		if r := s.requests[e.id]; r.status == model.StatusRequested {
			s.cancel(r, "max_request_to_assignment")
		}
	case eventPickupDeadline:
		s.cancel(s.requests[e.id], "max_request_to_pickup")
	case eventBatchTick:
		s.match(model.TriggerBatchTick)
		s.push(&event{at: e.at + *s.settings.BatchMatchingIntervalSeconds, priority: priorityBatchTick, id: "batch_tick", kind: eventBatchTick})
	}
}

func pick(override, setting *int64) *int64 {
	if override != nil {
		return override
	}
	return setting
}

// position returns where the car is now and its odometer, without committing.
func (s *Simulator) position(c *car) (model.Location, int64) {
	if c.phase != phaseDriving && c.phase != phaseRepositioning {
		return c.baseLocation, c.baseOdometer
	}
	to := c.repositionTarget
	if c.phase == phaseDriving {
		to = c.stops[0].location
	}
	if s.now >= c.legEndAt || c.legRemaining == 0 {
		if s.now >= c.legEndAt {
			return to, c.baseOdometer + c.legRemaining
		}
		return c.baseLocation, c.baseOdometer
	}
	elapsed := s.now - c.baseAt
	duration := c.legEndAt - c.baseAt
	driven := int64(math.Round(float64(c.legRemaining) * float64(elapsed) / float64(duration)))
	fraction := float64(driven) / float64(c.legRemaining)
	return model.Location{
		Lat: c.baseLocation.Lat + (to.Lat-c.baseLocation.Lat)*fraction,
		Lng: c.baseLocation.Lng + (to.Lng-c.baseLocation.Lng)*fraction,
	}, c.baseOdometer + driven
}

func (c *car) class() int {
	if c.seatsInUse > 0 {
		return classOnTrip
	}
	if len(c.stops) > 0 && c.stops[0].Type == model.Pickup {
		return classEnRoute
	}
	return classIdle
}

func (c *car) state() string {
	switch {
	case c.offline || !c.online:
		return model.StateOffline
	case c.goingOffline:
		return model.StateGoingOffline
	case c.seatsInUse > 0:
		return model.StateOnTrip
	case len(c.stops) > 0 && c.stops[0].Type == model.Pickup:
		return model.StateEnRoute
	case len(c.stops) > 0:
		return model.StateOnTrip
	case c.phase == phaseRepositioning:
		return model.StateRepositioning
	}
	return model.StateIdle
}

// sync commits the car's movement and time up to now.
func (s *Simulator) sync(c *car) {
	location, odometer := s.position(c)
	class := c.class()
	c.classMeters[class] += odometer - c.baseOdometer
	c.classSeconds[class] += s.now - c.accountedAt
	c.accountedAt = s.now
	if c.phase == phaseDriving || c.phase == phaseRepositioning {
		c.legRemaining -= odometer - c.baseOdometer
	}
	c.baseLocation, c.baseAt, c.baseOdometer = location, s.now, odometer
}

func (s *Simulator) dwell(t model.StopType) int64 {
	if t == model.Pickup {
		return s.settings.PickupDwellSeconds
	}
	return s.settings.DropoffDwellSeconds
}

// replan installs new stops on a synced car, routing from its current location
// (or from the stop it is dwelling at, which stays first).
func (s *Simulator) replan(c *car, stops []model.Stop) ([]model.Location, error) {
	if c.phase == phaseRepositioning {
		c.phase = phaseNone // any plan change ends a reposition where the car is
	}
	newStops := make([]planStop, len(stops))
	for i, stop := range stops {
		r := s.requests[stop.RequestID]
		location := r.input.Origin
		if stop.Type == model.Dropoff {
			location = r.input.Destination
		}
		newStops[i] = planStop{Stop: stop, location: location, seats: r.seats}
	}
	start := c.baseLocation
	routed := newStops
	clock := s.now
	if c.phase == phaseDwelling {
		newStops[0] = c.stops[0]
		start, routed, clock = newStops[0].location, newStops[1:], c.dwellEndAt
	}
	waypoints := make([]model.Location, len(routed))
	for i, stop := range routed {
		waypoints[i] = stop.location
	}
	legs, err := s.route(start, waypoints)
	if err != nil {
		return nil, err
	}
	for i := range routed {
		routed[i].legDistance, routed[i].legDuration = legs[i].DistanceMeters, legs[i].DurationSeconds
		clock += legs[i].DurationSeconds
		routed[i].etaAt = clock
		clock += s.dwell(routed[i].Type)
	}
	c.stops = newStops
	c.version++
	if len(newStops) == 0 {
		c.phase, c.planID = phaseNone, ""
	} else {
		c.planCount++
		c.planID = fmt.Sprintf("%s_plan_%d", c.input.SupplyID, c.planCount)
	}
	if c.phase != phaseDwelling && len(newStops) > 0 {
		c.phase, c.legRemaining, c.legEndAt = phaseDriving, newStops[0].legDistance, s.now+newStops[0].legDuration
	}
	s.schedule(c)
	return append([]model.Location{start}, waypoints...), nil
}

func (s *Simulator) schedule(c *car) {
	switch c.phase {
	case phaseDriving:
		priority := priorityPickup
		if c.stops[0].Type == model.Dropoff {
			priority = priorityDropoff
		}
		s.push(&event{at: c.legEndAt, priority: priority, id: c.input.SupplyID, kind: eventArrive, version: c.version})
	case phaseDwelling:
		priority := priorityPickup
		if c.stops[0].Type == model.Dropoff {
			priority = priorityDropoff
		}
		s.push(&event{at: c.dwellEndAt, priority: priority, id: c.input.SupplyID, kind: eventDwellDone, version: c.version})
	}
}

func (s *Simulator) arrive(c *car) {
	s.sync(c)
	stop := c.stops[0]
	c.baseLocation = stop.location
	c.phase, c.dwellEndAt, c.legRemaining = phaseDwelling, s.now+s.dwell(stop.Type), 0
	r := s.requests[stop.RequestID]
	if stop.Type == model.Pickup {
		at := s.now
		r.driverArrivedAt, r.status = &at, model.StatusDriverArrived
		s.log(c, "arrived_pickup", stop.RequestID, nil)
	} else {
		r.odometerAtDropoff = c.baseOdometer
		s.log(c, "arrived_dropoff", stop.RequestID, nil)
	}
	if c.dwellEndAt == s.now {
		s.finishStop(c)
		return
	}
	c.version++
	s.schedule(c)
}

func (s *Simulator) finishStop(c *car) {
	s.sync(c)
	stop := c.stops[0]
	c.stops = c.stops[1:]
	r := s.requests[stop.RequestID]
	at := s.now
	c.actions = append(c.actions, stop.Stop)
	if stop.Type == model.Pickup {
		c.aboard[stop.RequestID] = stop.seats
		c.seatsInUse += stop.seats
		for id := range c.aboard {
			if other := s.requests[id]; len(c.aboard)-1 > other.maxCoRiders {
				other.maxCoRiders = len(c.aboard) - 1
			}
		}
		r.pickupAt, r.status, r.odometerAtPickup = &at, model.StatusOnTrip, c.baseOdometer
		s.log(c, "pickup", stop.RequestID, nil)
	} else {
		delete(c.aboard, stop.RequestID)
		c.seatsInUse -= stop.seats
		r.dropoffAt, r.status = &at, model.StatusCompleted
		r.ridepoolType = ridepoolType(c.actions[r.actionIndexAtAssign:], stop.RequestID)
		c.served++
		s.log(c, "dropoff", stop.RequestID, nil)
	}
	c.version++
	if len(c.stops) > 0 {
		next := c.stops[0]
		c.phase, c.legRemaining, c.legEndAt = phaseDriving, next.legDistance, s.now+next.legDuration
		s.schedule(c)
		return
	}
	c.phase, c.planID = phaseNone, ""
	s.becameEmpty(c)
}

func (s *Simulator) becameEmpty(c *car) {
	if c.goingOffline {
		s.goOffline(c)
	} else {
		s.log(c, "idle", "", nil)
		s.reposition(c)
	}
}

// driverState is the driver behavior's view of a synced car.
func (s *Simulator) driverState(c *car) model.DriverState {
	location, _ := s.position(c)
	state := model.DriverState{Now: s.now, SupplyID: c.input.SupplyID, State: c.state(), Location: location,
		OnlineLocation: c.input.OnlineLocation, OnlineAt: c.input.OnlineAt, OfflineAt: c.input.OfflineAt,
		NumAvailableSeats: c.capacity, SeatsInUse: c.seatsInUse}
	for _, stop := range c.stops {
		state.Plan = append(state.Plan, model.Waypoint{RequestID: stop.RequestID, Type: stop.Type, Location: stop.location, EtaAt: stop.etaAt, NumRequestedSeats: stop.seats})
	}
	return state
}

// reposition asks the driver behavior where an idle, synced car should go
// and, if it names somewhere else, starts driving there.
func (s *Simulator) reposition(c *car) {
	if !c.online || c.offline || c.goingOffline || len(c.stops) > 0 {
		return
	}
	target := s.behavior.OnIdle(s.driverState(c))
	if target == nil || *target == c.baseLocation {
		return
	}
	legs, err := s.route(c.baseLocation, []model.Location{*target})
	if err != nil {
		s.log(c, "reposition_rejected", "", nil)
		return
	}
	c.phase, c.repositionTarget = phaseRepositioning, *target
	c.legRemaining, c.legEndAt = legs[0].DistanceMeters, s.now+legs[0].DurationSeconds
	c.version++
	s.push(&event{at: c.legEndAt, priority: priorityDropoff, id: c.input.SupplyID, kind: eventArrive, version: c.version})
	s.log(c, "reposition_started", "", []model.Location{c.baseLocation, *target})
}

func (s *Simulator) arriveReposition(c *car) {
	s.sync(c)
	c.baseLocation = c.repositionTarget
	c.phase, c.legRemaining = phaseNone, 0
	c.version++
	s.log(c, "reposition_arrived", "", nil)
	s.reposition(c)
}

func (s *Simulator) goOffline(c *car) {
	s.sync(c)
	at := s.now
	c.offline, c.goingOffline, c.offlineAt, c.phase = true, false, &at, phaseNone
	c.version++
	s.log(c, "offline", "", nil)
}

func ridepoolType(actions []model.Stop, requestID string) string {
	numbers := map[string]int{requestID: 0}
	next := 1
	out := ""
	for _, a := range actions {
		n, ok := numbers[a.RequestID]
		if !ok {
			n = next
			numbers[a.RequestID] = n
			next++
		}
		letter := "p"
		if a.Type == model.Dropoff {
			letter = "d"
		}
		out += fmt.Sprintf("%s%d", letter, n)
	}
	return out
}

func (s *Simulator) cancel(r *request, reason string) {
	at := s.now
	previous := r.status
	r.status, r.canceledAt, r.cancelReason = model.StatusCanceled, &at, reason
	if previous != model.StatusAssigned && previous != model.StatusDriverArrived {
		return
	}
	c := s.cars[r.supplyID]
	s.sync(c)
	var remaining []model.Stop
	for _, stop := range c.stops {
		if stop.RequestID != r.input.RequestID {
			remaining = append(remaining, stop.Stop)
		}
	}
	if c.phase == phaseDwelling && c.stops[0].RequestID == r.input.RequestID {
		c.phase = phaseNone // the car leaves from the stop it was dwelling at
	}
	planned, err := s.replan(c, remaining)
	s.log(c, "stop_removed", r.input.RequestID, nil)
	if err != nil {
		return
	}
	if len(remaining) == 0 {
		s.becameEmpty(c)
	} else {
		s.log(c, "plan_updated", "", planned)
	}
}
