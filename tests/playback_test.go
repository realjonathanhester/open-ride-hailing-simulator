package tests

import (
	"strconv"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/viewer"
)

// Playback component tests: /api/playback returns car segments and request
// spans for one window of simulated time (tooling spec section 6).

type playbackResponse struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
	Cars []struct {
		SupplyID string `json:"supply_id"`
		Link     string `json:"link"`
		Segments []struct {
			StartAt      int64  `json:"start_at"`
			EndAt        int64  `json:"end_at"`
			State        string `json:"state"`
			RidersAboard int    `json:"riders_aboard"`
		} `json:"segments"`
	} `json:"cars"`
	Requests []struct {
		RequestID   string `json:"request_id"`
		Link        string `json:"link"`
		RequestedAt int64  `json:"requested_at"`
		PickupAt    *int64 `json:"pickup_at"`
		DropoffAt   *int64 `json:"dropoff_at"`
	} `json:"requests"`
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// Case 1 with the car at Union Square: idle, en route, then on trip.
func TestPlaybackSegmentsFollowCarStates(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "base_default", "one_car_at_union_square", "single_trip")})
	var playback playbackResponse
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/api/playback?from="+at(0)+"&to="+at(3600), &playback)), "200")
	car := playback.Cars[0]
	assertEquals(t, car.Link, "/supply/car_at_union_square")
	assertEquals(t, strconv.Itoa(len(car.Segments)), "3")
	assertEquals(t, car.Segments[0].State, "idle")
	assertEquals(t, car.Segments[1].State, "en_route")
	assertEquals(t, itoa(car.Segments[1].EndAt), at(551)) // driver arrives at Kezar Stadium
	assertEquals(t, car.Segments[2].State, "on_trip")
	assertEquals(t, strconv.Itoa(car.Segments[2].RidersAboard), "1")
	request := playback.Requests[0]
	assertEquals(t, request.Link, "/trips/kezar_stadium_to_coit_tower")
	assertEquals(t, itoa(*request.PickupAt), at(551))
	assertEquals(t, itoa(*request.DropoffAt), at(1145))
}

// Case 4: the second rider shows as two riders aboard and is listed second.
func TestPlaybackShowsPooledRiders(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")})
	var playback playbackResponse
	get(t, server.URL+"/api/playback?from="+at(0)+"&to="+at(3600), &playback)
	assertEquals(t, strconv.Itoa(playback.Cars[0].Segments[3].RidersAboard), "2") // Painted Ladies to Union Square
	assertEquals(t, playback.Requests[1].RequestID, "painted_ladies_to_union_square")
}

// Case 4 at T+0: the first rider is requested and picked up at the run start, so
// the flag is correct at T+0 and the car's on_trip segment starts exactly then.
func TestPlaybackPickupAtRunStart(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")})
	var playback playbackResponse
	get(t, server.URL+"/api/playback?from="+at(0)+"&to="+at(900), &playback)
	first := playback.Requests[0]
	assertEquals(t, first.RequestID, "kezar_stadium_to_coit_tower")
	assertEquals(t, itoa(first.RequestedAt), at(60)) // the viewer's T+0
	assertEquals(t, itoa(*first.PickupAt), at(60))
	segments := playback.Cars[0].Segments
	assertEquals(t, itoa(segments[0].EndAt), at(60)) // idle until the pickup
	assertEquals(t, segments[0].State, "idle")
	assertEquals(t, itoa(segments[1].StartAt), at(60))
	assertEquals(t, segments[1].State, "on_trip")
	second := playback.Requests[1]
	assertEquals(t, second.RequestID, "painted_ladies_to_union_square")
	assertEquals(t, itoa(*second.PickupAt), at(296)) // viewer T+236, so the origin icon is gone by T+250
}

func TestPlaybackWindowLeavesOutEarlierSegmentsAndFinishedRequests(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "pooling_enabled", "one_car_at_kezar_stadium", "simple_pool")})
	var playback playbackResponse
	get(t, server.URL+"/api/playback?from="+at(600)+"&to="+at(700), &playback)
	assertEquals(t, strconv.Itoa(len(playback.Cars[0].Segments)), "1") // only the last leg to Coit Tower
	assertEquals(t, strconv.Itoa(len(playback.Requests)), "1")         // painted_ladies dropped off at T+554
	assertEquals(t, playback.Requests[0].RequestID, "kezar_stadium_to_coit_tower")
}

func TestPlaybackWindowDefaultsAndCaps(t *testing.T) {
	server := serve(t, viewer.Config{Run: simulateForViewer(t, "base_default", "one_car_at_union_square", "single_trip")})
	var playback playbackResponse
	get(t, server.URL+"/api/playback", &playback)
	assertEquals(t, itoa(playback.From), at(60)) // simulation_start_at is the first request
	assertEquals(t, itoa(playback.To), at(960))  // 900 s default window
	get(t, server.URL+"/api/playback?from="+at(0)+"&to="+at(100000), &playback)
	assertEquals(t, itoa(playback.To), at(1145)) // capped at simulation_end_at
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/api/playback?from="+at(10)+"&to="+at(0), nil)), "400")
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/api/playback?run=b", nil)), "404") // no -compare run
}
