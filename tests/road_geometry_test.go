package tests

import (
	"math"
	"strconv"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/polyline"
	"github.com/realjonathanhester/ride-hailing-simulator/routing"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
	"github.com/realjonathanhester/ride-hailing-simulator/simulator"
)

func init() {
	if err := runner.RegisterRouter("manhattan", func(model.Settings) (model.Router, error) { return manhattanRouter{bent: true}, nil }); err != nil {
		panic(err)
	}
}

// manhattanRouter drives every leg as an L: along the start's latitude to
// the target's longitude, then along that longitude. Distances and
// durations are the same whether or not it reports the bend, so only the
// geometry differs between bent and straight runs.
type manhattanRouter struct{ bent bool }

func (r manhattanRouter) Route(start model.Location, waypoints []model.Location, departAt int64) ([]model.Leg, error) {
	var legs []model.Leg
	from := start
	for _, to := range waypoints {
		corner := model.Location{Lat: from.Lat, Lng: to.Lng}
		distance := routing.HaversineMeters(from, corner) + routing.HaversineMeters(corner, to)
		duration := int64(math.Round(float64(distance) / 10))
		points := []model.Location{from, to}
		if r.bent {
			points = []model.Location{from, corner, to}
		}
		legs = append(legs, model.Leg{Segments: []model.Segment{{Polyline: points, DistanceMeters: distance, DurationSeconds: duration}},
			DistanceMeters: distance, DurationSeconds: duration})
		from = to
	}
	return legs, nil
}

var (
	kezarStadium  = model.Location{Lat: 37.767263, Lng: -122.456743}
	paintedLadies = model.Location{Lat: 37.776120, Lng: -122.432700}
)

// The car at Union Square drives west then south to Kezar Stadium for A
// (about 4339 m then 2304 m); B is requested at T+560, past the bend, so
// the car is rerouted mid-leg with pooling on.
func runManhattan(t *testing.T, bent, withB bool) *simulator.Result {
	t.Helper()
	settings := model.Settings{PoolingEnabled: true, ReassignmentEnabled: true, DefaultNumRequestedSeats: 1, DefaultNumAvailableSeats: 4,
		AverageSpeedMetersPerSecond: 10, MatchingHook: "greedy_nearest_eta", RoutingHook: "manhattan"}
	requests := []model.Request{
		{RequestID: "a_kezar_to_coit", RequestedAt: T + 60, Origin: kezarStadium, Destination: coitTower},
		{RequestID: "b_painted_ladies_to_union_square", RequestedAt: T + 560, Origin: paintedLadies, Destination: unionSquare},
	}
	if !withB {
		requests = requests[:1]
	}
	supply := []model.Supply{{SupplyID: "car", OnlineAt: T, OnlineLocation: unionSquare}}
	result, err := simulator.New(settings, manhattanRouter{bent: bent}, matching.NewGreedy()).Run(requests, supply)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// assertAxisAligned checks every piece of an encoded polyline runs due
// north-south or east-west, as the manhattan router's roads do.
func assertAxisAligned(t *testing.T, what, encoded string) {
	t.Helper()
	points := polyline.Decode(encoded)
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		if math.Abs(a.Lat-b.Lat) > 1.5e-5 && math.Abs(a.Lng-b.Lng) > 1.5e-5 {
			t.Errorf("%s leaves the road between %v and %v: %s", what, a, b, encoded)
		}
	}
}

func TestCarsFollowRouterGeometry(t *testing.T) {
	result := runManhattan(t, true, true)
	var rerouted []string
	for _, row := range result.SupplyEvents {
		if row[1] == at(560) && row[2] == "plan_updated" {
			rerouted = row
		}
		assertAxisAligned(t, row[2]+" driven_polyline at "+row[1], row[10])
		assertAxisAligned(t, row[2]+" planned_polyline at "+row[1], row[11])
	}
	if rerouted == nil {
		t.Fatalf("no plan_updated at T+560")
	}
	// Mid-leg, past the bend: on the southbound road at Kezar's longitude.
	assertEquals(t, rerouted[4], "-122.456743")
	lat, _ := strconv.ParseFloat(rerouted[3], 64)
	if lat <= kezarStadium.Lat || lat >= unionSquare.Lat {
		t.Errorf("rerouted at lat %v, want between Kezar and Union Square", lat)
	}
	driven := polyline.Decode(rerouted[10])
	if len(driven) != 3 {
		t.Fatalf("driven_polyline at the reroute = %v, want start, bend, here", driven)
	}
	assertEquals(t, strconv.FormatFloat(driven[1].Lat, 'f', 5, 64), "37.78799") // the bend west of Union Square
	assertEquals(t, strconv.FormatFloat(driven[1].Lng, 'f', 5, 64), "-122.45674")
	planned := polyline.Decode(rerouted[11])
	if len(planned) < 3 {
		t.Fatalf("planned_polyline = %v, want road geometry", planned)
	}
	for _, summary := range result.SupplySummary {
		assertAxisAligned(t, "full_polyline", summary[12])
	}
}

// Road geometry changes positions and polylines only: with no mid-leg
// reroute (which routes from a different point), trips, odometers and supply
// meters are the same as with straight geometry for the same legs.
func TestRouterGeometryDoesNotChangeMeters(t *testing.T) {
	bent, straight := runManhattan(t, true, false), runManhattan(t, false, false)
	assertEquals(t, csvText(bent.Trips), csvText(straight.Trips))
	if len(bent.SupplyEvents) != len(straight.SupplyEvents) {
		t.Fatalf("event rows %d vs %d", len(bent.SupplyEvents), len(straight.SupplyEvents))
	}
	for i := range bent.SupplyEvents {
		b, s := bent.SupplyEvents[i], straight.SupplyEvents[i]
		assertEquals(t, b[1]+" "+b[2]+" "+b[9], s[1]+" "+s[2]+" "+s[9]) // at, action, odometer
	}
	assertEquals(t, csvText([][]string{bent.SupplySummary[0][:12]}), csvText([][]string{straight.SupplySummary[0][:12]}))
}

func csvText(rows [][]string) string {
	out := ""
	for _, row := range rows {
		for _, cell := range row {
			out += cell + ","
		}
		out += "\n"
	}
	return out
}

// Reposition legs follow the road too, and the viewer's playback moves cars
// along the driven geometry rather than straight between rows.
func TestRepositionAndPlaybackFollowRouterGeometry(t *testing.T) {
	o := simulate(t, "manhattan_return_to_origin", "one_car_at_union_square", "single_trip")
	started := o.eventsWithAction("car_at_union_square", "reposition_started")
	arrived := o.eventsWithAction("car_at_union_square", "reposition_arrived")
	if len(started) != 1 || len(arrived) != 1 {
		t.Fatalf("want one reposition, got %d started, %d arrived", len(started), len(arrived))
	}
	if n := len(polyline.Decode(started[0]["planned_polyline"])); n != 3 {
		t.Errorf("reposition planned_polyline has %d points, want 3 (with the bend)", n)
	}
	if n := len(polyline.Decode(arrived[0]["driven_polyline"])); n != 3 {
		t.Errorf("reposition driven_polyline has %d points, want 3 (with the bend)", n)
	}
	for _, event := range o.events("car_at_union_square") {
		assertAxisAligned(t, event["action"]+" driven_polyline", event["driven_polyline"])
		assertAxisAligned(t, event["action"]+" planned_polyline", event["planned_polyline"])
	}
	assertAxisAligned(t, "full_polyline", o.supplySummary("car_at_union_square")["full_polyline"])

	run := simulateForViewer(t, "manhattan_return_to_origin", "one_car_at_union_square", "single_trip")
	playback := run.Playback(T, T+3600)
	bent := 0
	for _, segment := range playback.Cars[0].Segments {
		if len(segment.Path) > 0 {
			bent++
			if segment.Path[0] != segment.From || segment.Path[len(segment.Path)-1] != segment.To {
				t.Errorf("playback path %v does not run from %v to %v", segment.Path, segment.From, segment.To)
			}
		}
	}
	if bent < 3 { // to the pickup, to the dropoff, back to the origin
		t.Errorf("playback segments with road paths = %d, want at least 3", bent)
	}
}
