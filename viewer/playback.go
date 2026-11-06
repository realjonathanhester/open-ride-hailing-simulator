package viewer

import (
	"sort"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/polyline"
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
	// Path is the road geometry driven from From to To (the next row's
	// driven_polyline), set only when it has bends; playback moves the car
	// along it. Absent means a straight line.
	Path []model.Location `json:"path,omitempty"`
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

// playbackIndex is every car segment and request span of a run, built once.
type playbackIndex struct {
	cars     []PlaybackCar     // every segment of every car, by supply_id
	requests []PlaybackRequest // every request with known stops, sorted
	visible  []int64           // visibleUntil for each entry of requests
}

func (r *Run) playbackIndex() *playbackIndex {
	r.playbackOnce.Do(func() {
		index := &playbackIndex{}
		for _, supplyID := range r.supplyIDs {
			car := PlaybackCar{SupplyID: supplyID, Link: "/supply/" + supplyID}
			rows := r.eventsBySupply[supplyID]
			aboard := 0
			for k, i := range rows {
				event := r.Events[i]
				start, _ := integer(event, "at")
				here := location(event)
				var end int64
				var there *model.Location
				var path []model.Location
				if k+1 < len(rows) {
					next := r.Events[rows[k+1]]
					end, _ = integer(next, "at")
					there = location(next)
					if points := polyline.Decode(next["driven_polyline"]); len(points) > 2 && there != nil && here != nil {
						// Exact endpoints, encoded (1e-5) bends in between.
						path = append(append([]model.Location{*here}, points[1:len(points)-1]...), *there)
					}
				} else if event["state_after"] != "offline" {
					end, there = r.endAt(), here
				}
				switch event["action"] {
				case "pickup":
					aboard++
				case "dropoff":
					aboard--
				}
				if here == nil || there == nil || end <= start {
					continue
				}
				car.Segments = append(car.Segments, CarSegment{StartAt: start, EndAt: end, From: *here, To: *there, State: event["state_after"], RidersAboard: aboard, Path: path})
			}
			index.cars = append(index.cars, car)
		}
		ends := r.requests()
		for _, trip := range r.Trips {
			id := trip["request_id"]
			facts, known := ends[id]
			requested, ok := integer(trip, "requested_at")
			if !known || !ok {
				continue
			}
			index.requests = append(index.requests, PlaybackRequest{RequestID: id, Link: "/trips/" + id, Status: trip["status"], SupplyID: trip["supply_id"],
				Origin: facts.ends[0], Destination: facts.ends[1], RequestedAt: requested,
				PickupAt: optionalInteger(trip, "pickup_at"), DropoffAt: optionalInteger(trip, "dropoff_at"), CanceledAt: optionalInteger(trip, "canceled_at")})
		}
		sort.SliceStable(index.requests, func(i, j int) bool {
			a, b := index.requests[i], index.requests[j]
			if a.RequestedAt != b.RequestedAt {
				return a.RequestedAt < b.RequestedAt
			}
			return a.RequestID < b.RequestID
		})
		for _, request := range index.requests {
			visibleUntil := r.endAt()
			switch {
			case request.DropoffAt != nil:
				visibleUntil = *request.DropoffAt
			case request.CanceledAt != nil:
				visibleUntil = *request.CanceledAt
			}
			index.visible = append(index.visible, visibleUntil)
		}
		r.playbackCache = index
	})
	return r.playbackCache
}

// Playback returns the car segments and request spans overlapping [from, to].
// It filters a per-run index built on first use instead of rescanning the
// supply event log for every window.
func (r *Run) Playback(from, to int64) Playback {
	playback := Playback{From: from, To: to, SimulationStartAt: r.startAt(), SimulationEndAt: r.endAt(), Cars: []PlaybackCar{}, Requests: []PlaybackRequest{}}
	index := r.playbackIndex()
	for _, all := range index.cars {
		car := PlaybackCar{SupplyID: all.SupplyID, Link: all.Link, Segments: []CarSegment{}}
		for _, segment := range all.Segments {
			if overlaps(segment.StartAt, segment.EndAt, from, to) {
				car.Segments = append(car.Segments, segment)
			}
		}
		if len(car.Segments) > 0 {
			playback.Cars = append(playback.Cars, car)
		}
	}
	for k, request := range index.requests {
		if request.RequestedAt > to {
			break // sorted by requested_at
		}
		if overlaps(request.RequestedAt, index.visible[k], from, to) {
			playback.Requests = append(playback.Requests, request)
		}
	}
	return playback
}
