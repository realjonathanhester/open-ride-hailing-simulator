package viewer

import (
	"sort"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
)

// Playback window sizes in simulated seconds (tooling spec section 6).
const (
	DefaultPlaybackWindowSeconds = 900
	MaxPlaybackWindowSeconds     = 3600
)

// CarSegment is one car's movement between two consecutive SupplyEvents rows.
type CarSegment struct {
	StartAt      int64          `json:"start_at"`
	EndAt        int64          `json:"end_at"`
	From         model.Location `json:"from"`
	To           model.Location `json:"to"`
	State        string         `json:"state"`
	RidersAboard int            `json:"riders_aboard"`
}

// PlaybackCar is one car's segments in a playback window.
type PlaybackCar struct {
	SupplyID string       `json:"supply_id"`
	Link     string       `json:"link"`
	Segments []CarSegment `json:"segments"`
}

// PlaybackRequest is one request's visible span.
type PlaybackRequest struct {
	RequestID   string         `json:"request_id"`
	Link        string         `json:"link"`
	Status      string         `json:"status"`
	SupplyID    string         `json:"supply_id"`
	Origin      model.Location `json:"origin"`
	Destination model.Location `json:"destination"`
	RequestedAt int64          `json:"requested_at"`
	PickupAt    *int64         `json:"pickup_at,omitempty"`
	DropoffAt   *int64         `json:"dropoff_at,omitempty"`
	CanceledAt  *int64         `json:"canceled_at,omitempty"`
}

// Playback is one window of the fleet timeline.
type Playback struct {
	From              int64             `json:"from"`
	To                int64             `json:"to"`
	SimulationStartAt int64             `json:"simulation_start_at"`
	SimulationEndAt   int64             `json:"simulation_end_at"`
	Cars              []PlaybackCar     `json:"cars"`
	Requests          []PlaybackRequest `json:"requests"`
}

func optionalInteger(row Row, field string) *int64 {
	if v, ok := integer(row, field); ok {
		return &v
	}
	return nil
}

func overlaps(start, end, from, to int64) bool { return start <= to && end >= from }

// Playback returns the car segments and request spans overlapping [from, to].
func (r *Run) Playback(from, to int64) Playback {
	playback := Playback{From: from, To: to, SimulationStartAt: r.startAt(), SimulationEndAt: r.endAt(), Cars: []PlaybackCar{}, Requests: []PlaybackRequest{}}
	for _, supplyID := range r.supplyIDs {
		car := PlaybackCar{SupplyID: supplyID, Link: "/supply/" + supplyID, Segments: []CarSegment{}}
		rows := r.eventsBySupply[supplyID]
		aboard := 0
		for k, i := range rows {
			event := r.Events[i]
			start, _ := integer(event, "at")
			here := location(event)
			var end int64
			var there *model.Location
			if k+1 < len(rows) {
				next := r.Events[rows[k+1]]
				end, _ = integer(next, "at")
				there = location(next)
			} else if event["state_after"] != "offline" {
				end, there = r.endAt(), here
			}
			switch event["action"] {
			case "pickup":
				aboard++
			case "dropoff":
				aboard--
			}
			if here == nil || there == nil || end <= start || !overlaps(start, end, from, to) {
				continue
			}
			car.Segments = append(car.Segments, CarSegment{StartAt: start, EndAt: end, From: *here, To: *there, State: event["state_after"], RidersAboard: aboard})
		}
		if len(car.Segments) > 0 {
			playback.Cars = append(playback.Cars, car)
		}
	}
	ends := r.allRequestEnds()
	for _, trip := range r.Trips {
		id := trip["request_id"]
		stops, known := ends[id]
		requested, ok := integer(trip, "requested_at")
		if !known || !ok {
			continue
		}
		request := PlaybackRequest{RequestID: id, Link: "/trips/" + id, Status: trip["status"], SupplyID: trip["supply_id"],
			Origin: stops[0], Destination: stops[1], RequestedAt: requested,
			PickupAt: optionalInteger(trip, "pickup_at"), DropoffAt: optionalInteger(trip, "dropoff_at"), CanceledAt: optionalInteger(trip, "canceled_at")}
		visibleUntil := r.endAt()
		switch {
		case request.DropoffAt != nil:
			visibleUntil = *request.DropoffAt
		case request.CanceledAt != nil:
			visibleUntil = *request.CanceledAt
		}
		if overlaps(requested, visibleUntil, from, to) {
			playback.Requests = append(playback.Requests, request)
		}
	}
	sort.SliceStable(playback.Requests, func(i, j int) bool {
		a, b := playback.Requests[i], playback.Requests[j]
		if a.RequestedAt != b.RequestedAt {
			return a.RequestedAt < b.RequestedAt
		}
		return a.RequestID < b.RequestID
	})
	return playback
}

// allRequestEnds maps request_id to [origin, destination] from every plan, in one pass.
func (r *Run) allRequestEnds() map[string][2]model.Location {
	ends := map[string][2]model.Location{}
	for _, event := range r.Events {
		for _, stop := range parsePlan(event["plan"]) {
			pair := ends[stop.RequestID]
			if stop.Type == "pickup" {
				pair[0] = model.Location{Lat: stop.Lat, Lng: stop.Lng}
			} else {
				pair[1] = model.Location{Lat: stop.Lat, Lng: stop.Lng}
			}
			ends[stop.RequestID] = pair
		}
	}
	return ends
}
