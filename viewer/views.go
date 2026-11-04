package viewer

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/polyline"
)

// PlanStop is one stop in a plan_updated row's plan cell.
type PlanStop struct {
	RequestID string  `json:"request_id"`
	Type      string  `json:"type"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	EtaAt     int64   `json:"eta_at"`
}

func parsePlan(cell string) []PlanStop {
	var stops []PlanStop
	if cell != "" {
		_ = json.Unmarshal([]byte(cell), &stops)
	}
	return stops
}

func planContains(cell, requestID string) bool {
	for _, stop := range parsePlan(cell) {
		if stop.RequestID == requestID {
			return true
		}
	}
	return false
}

func location(row Row) *model.Location {
	lat, okLat := decimal(row, "lat")
	lng, okLng := decimal(row, "lng")
	if !okLat || !okLng {
		return nil
	}
	return &model.Location{Lat: lat, Lng: lng}
}

// PathSegment is a decoded piece of driven path.
type PathSegment struct {
	Kind   string           `json:"kind"`
	Points []model.Location `json:"points"`
}

// RidepoolToken is one p/d token of a ridepool_type, labeled with its request.
type RidepoolToken struct {
	Token     string `json:"token"`
	RequestID string `json:"request_id"`
	Link      string `json:"link"`
}

// TripView is the trip page.
type TripView struct {
	Trip              Row                 `json:"trip"`
	Derived           map[string]*float64 `json:"derived"`
	Shared            bool                `json:"shared"`
	Ridepool          []RidepoolToken     `json:"ridepool"`
	Path              []PathSegment       `json:"path"`
	PlannedRoute      []model.Location    `json:"planned_route"`
	CarAtAssignment   *model.Location     `json:"car_at_assignment"`
	CarAtCancellation *model.Location     `json:"car_at_cancellation"`
	Origin            model.Location      `json:"origin"`
	Destination       model.Location      `json:"destination"`
	SupplyLink        string              `json:"supply_link"`
	EarlierSupplyIDs  []string            `json:"earlier_supply_ids"`
	CompareLink       string              `json:"compare_link,omitempty"`
}

func diff(row Row, a, b string) *float64 {
	x, okA := integer(row, a)
	y, okB := integer(row, b)
	if !okA || !okB {
		return nil
	}
	return value(float64(x - y))
}

// TripView builds one trip page; ok is false when the request is unknown.
func (r *Run) TripView(requestID string) (TripView, bool) {
	trip, ok := r.tripByID[requestID]
	if !ok {
		return TripView{}, false
	}
	view := TripView{Trip: trip, Derived: map[string]*float64{}, Ridepool: []RidepoolToken{}, Path: []PathSegment{}, EarlierSupplyIDs: []string{}}
	co, _ := integer(trip, "max_co_riders")
	view.Shared = co > 0
	view.Derived["extra_meters"] = diff(trip, "trip_distance_meters", "direct_distance_meters")
	if duration, ok := integer(trip, "trip_duration_seconds"); ok && trip["status"] == "completed" {
		direct, _ := integer(trip, "direct_duration_seconds")
		view.Derived["extra_seconds"] = value(float64(duration-direct) - r.dropoffDwellSeconds())
	} else {
		view.Derived["extra_seconds"] = nil
	}
	view.Derived["assignment_delay_seconds"] = diff(trip, "first_assigned_at", "requested_at")
	if arrived, ok := integer(trip, "driver_arrived_at"); ok {
		assigned, _ := integer(trip, "assigned_at")
		eta, _ := integer(trip, "eta_at_assignment_seconds")
		view.Derived["eta_error_seconds"] = value(float64(arrived - (assigned + eta)))
	} else {
		view.Derived["eta_error_seconds"] = nil
	}

	supplyID := trip["supply_id"]
	if supplyID != "" {
		view.SupplyLink = "/supply/" + supplyID
		if assigned := trip["assigned_at"]; assigned != "" {
			view.SupplyLink += "?at=" + assigned
		}
	}
	seen := map[string]bool{}
	for _, event := range r.Events {
		id := event["supply_id"]
		if id != supplyID && !seen[id] && event["action"] == "plan_updated" && planContains(event["plan"], requestID) {
			seen[id] = true
			view.EarlierSupplyIDs = append(view.EarlierSupplyIDs, id)
		}
	}
	sort.Strings(view.EarlierSupplyIDs)

	// The final assignment is the last plan_updated row on the final car at
	// assigned_at whose plan contains this request.
	rows := r.eventsBySupply[supplyID]
	assignmentRow := -1
	for k, i := range rows {
		event := r.Events[i]
		if event["action"] == "plan_updated" && event["at"] == trip["assigned_at"] && planContains(event["plan"], requestID) {
			assignmentRow = k
		}
	}
	if assignmentRow >= 0 {
		view.CarAtAssignment = location(r.Events[rows[assignmentRow]])
		numbers := map[string]int{requestID: 0}
		next := 1
		aboard := false
		for _, i := range rows[assignmentRow+1:] {
			event := r.Events[i]
			action := event["action"]
			if points := polyline.Decode(event["driven_polyline"]); len(points) > 0 {
				kind := "to_pickup"
				if aboard {
					kind = "on_trip"
				}
				view.Path = append(view.Path, PathSegment{Kind: kind, Points: points})
			}
			if action == "pickup" || action == "dropoff" {
				id := event["request_id"]
				n, known := numbers[id]
				if !known {
					n = next
					numbers[id] = n
					next++
				}
				token := RidepoolToken{Token: action[:1] + strconv.Itoa(n), RequestID: id}
				if id != requestID {
					token.Link = "/trips/" + id
				}
				if trip["status"] == "completed" {
					view.Ridepool = append(view.Ridepool, token)
				}
				if id == requestID {
					if action == "pickup" {
						aboard = true
					} else {
						break
					}
				}
			}
			if action == "stop_removed" && event["request_id"] == requestID {
				break
			}
		}
	}
	if trip["status"] == "canceled" && supplyID != "" {
		for _, i := range rows {
			event := r.Events[i]
			if event["action"] == "plan_updated" && event["at"] <= trip["canceled_at"] && planContains(event["plan"], requestID) {
				view.PlannedRoute = polyline.Decode(event["planned_polyline"])
			}
			if event["action"] == "stop_removed" && event["request_id"] == requestID {
				view.CarAtCancellation = location(event)
			}
		}
		view.Path = []PathSegment{} // the car never carried this rider
	}
	view.Origin, view.Destination = r.requestEnds(requestID)
	return view, true
}

// requestEnds finds the origin and destination from any plan that lists them.
func (r *Run) requestEnds(requestID string) (model.Location, model.Location) {
	var origin, destination model.Location
	for _, event := range r.Events {
		for _, stop := range parsePlan(event["plan"]) {
			if stop.RequestID == requestID {
				if stop.Type == "pickup" {
					origin = model.Location{Lat: stop.Lat, Lng: stop.Lng}
				} else {
					destination = model.Location{Lat: stop.Lat, Lng: stop.Lng}
				}
			}
		}
	}
	return origin, destination
}

// StateSegment is a piece of a car's driven path, colored by state.
type StateSegment struct {
	State        string           `json:"state"`
	RidersAboard int              `json:"riders_aboard"`
	Points       []model.Location `json:"points"`
}

// StateSpan is one interval of the state band.
type StateSpan struct {
	State   string `json:"state"`
	StartAt int64  `json:"start_at"`
	EndAt   int64  `json:"end_at"`
}

// TimelineEvent is one supply event log row, with its plan parsed.
type TimelineEvent struct {
	Row          Row              `json:"row"`
	Plan         []PlanStop       `json:"plan,omitempty"`
	PlannedRoute []model.Location `json:"planned_route,omitempty"`
}

// SupplyTrip is one request this car was assigned at any time.
type SupplyTrip struct {
	RequestID      string `json:"request_id"`
	Status         string `json:"status"`
	RidepoolType   string `json:"ridepool_type"`
	ReassignedAway bool   `json:"reassigned_away"`
	Link           string `json:"link"`
	Trip           Row    `json:"trip"` // full Trips row, for map info windows
}

// SupplyView is the supply page.
type SupplyView struct {
	Summary           Row             `json:"summary"`
	OnlineSeconds     int64           `json:"online_seconds"`
	TotalMeters       int64           `json:"total_meters"`
	MaxRidersAboard   int             `json:"max_riders_aboard"`
	PlanRejectedCount int             `json:"plan_rejected_count"`
	Segments          []StateSegment  `json:"segments"`
	States            []StateSpan     `json:"states"`
	Events            []TimelineEvent `json:"events"`
	Trips             []SupplyTrip    `json:"trips"`
	PositionAt        *model.Location `json:"position_at,omitempty"`
}

// SupplyView builds one car's page. at, when non-nil, adds its position then.
func (r *Run) SupplyView(supplyID string, at *int64) (SupplyView, bool) {
	summary, ok := r.supplyByID[supplyID]
	if !ok {
		return SupplyView{}, false
	}
	view := SupplyView{Summary: summary, OnlineSeconds: r.onlineSeconds(summary), TotalMeters: totalMeters(summary),
		MaxRidersAboard: r.maxRidersAboard(supplyID), Segments: []StateSegment{}, States: []StateSpan{}, Events: []TimelineEvent{}, Trips: []SupplyTrip{}}
	rows := r.eventsBySupply[supplyID]
	aboard := 0
	seenTrips := map[string]bool{}
	for k, i := range rows {
		event := r.Events[i]
		timelineEvent := TimelineEvent{Row: event}
		if event["action"] == "plan_updated" {
			timelineEvent.Plan = parsePlan(event["plan"])
			timelineEvent.PlannedRoute = polyline.Decode(event["planned_polyline"])
			for _, stop := range timelineEvent.Plan {
				if !seenTrips[stop.RequestID] {
					seenTrips[stop.RequestID] = true
					trip := r.tripByID[stop.RequestID]
					view.Trips = append(view.Trips, SupplyTrip{RequestID: stop.RequestID, Status: trip["status"], RidepoolType: trip["ridepool_type"],
						ReassignedAway: trip["supply_id"] != supplyID, Link: "/trips/" + stop.RequestID, Trip: trip})
				}
			}
		}
		if event["action"] == "plan_rejected" {
			view.PlanRejectedCount++
		}
		view.Events = append(view.Events, timelineEvent)
		if k > 0 {
			previous := r.Events[rows[k-1]]
			if points := polyline.Decode(event["driven_polyline"]); len(points) > 0 {
				view.Segments = append(view.Segments, StateSegment{State: previous["state_after"], RidersAboard: aboard, Points: points})
			}
			start, _ := integer(previous, "at")
			end, _ := integer(event, "at")
			if end > start {
				view.States = appendSpan(view.States, StateSpan{previous["state_after"], start, end})
			}
		}
		switch event["action"] {
		case "pickup":
			aboard++
		case "dropoff":
			aboard--
		}
	}
	if len(rows) > 0 {
		last := r.Events[rows[len(rows)-1]]
		start, _ := integer(last, "at")
		if last["state_after"] != "offline" && r.endAt() > start {
			view.States = appendSpan(view.States, StateSpan{last["state_after"], start, r.endAt()})
		}
	}
	if at != nil {
		view.PositionAt = r.positionAt(rows, *at)
	}
	return view, true
}

func appendSpan(spans []StateSpan, span StateSpan) []StateSpan {
	if n := len(spans); n > 0 && spans[n-1].State == span.State && spans[n-1].EndAt == span.StartAt {
		spans[n-1].EndAt = span.EndAt
		return spans
	}
	return append(spans, span)
}

// positionAt interpolates linearly in time between the two rows around at.
// Between consecutive rows the car drives one straight constant-speed piece
// (or stands still), so this matches the simulator's interpolation rule.
func (r *Run) positionAt(rows []int, at int64) *model.Location {
	var previous Row
	for _, i := range rows {
		event := r.Events[i]
		eventAt, _ := integer(event, "at")
		if eventAt >= at {
			if previous == nil || eventAt == at {
				return location(event)
			}
			from, to := location(previous), location(event)
			previousAt, _ := integer(previous, "at")
			fraction := float64(at-previousAt) / float64(eventAt-previousAt)
			return &model.Location{Lat: from.Lat + (to.Lat-from.Lat)*fraction, Lng: from.Lng + (to.Lng-from.Lng)*fraction}
		}
		previous = event
	}
	if previous != nil {
		return location(previous)
	}
	return nil
}
