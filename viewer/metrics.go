package viewer

import (
	"math"
	"sort"
	"strings"
)

// DefinitionsURL is the base for metric definition links.
const DefinitionsURL = "https://github.com/realjonathanhester/ride-hailing-simulator/blob/main/docs/tooling_spec.md"

// Metric is one dashboard number. Value is nil when undefined (n/a).
type Metric struct {
	Name       string   `json:"name"`
	Group      string   `json:"group"`
	Value      *float64 `json:"value"`
	Better     string   `json:"better"` // "higher", "lower" or "" (neutral)
	Definition string   `json:"definition_url"`
}

// Bucket is one histogram bar covering [lower, upper).
type Bucket struct {
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
	Count int     `json:"count"`
}

// RidepoolCount is one row of the ridepool_type breakdown.
type RidepoolCount struct {
	RidepoolType string  `json:"ridepool_type"`
	Count        int     `json:"count"`
	Share        float64 `json:"share"`
}

// TimelinePoint counts open requests and cars by state at one moment.
type TimelinePoint struct {
	At           int64          `json:"at"`
	OpenRequests int            `json:"open_requests"`
	CarsByState  map[string]int `json:"cars_by_state"`
}

// Dashboard is the aggregate results page.
type Dashboard struct {
	StartAt       int64               `json:"simulation_start_at"`
	EndAt         int64               `json:"simulation_end_at"`
	Metrics       []Metric            `json:"metrics"`
	Distributions map[string][]Bucket `json:"distributions"`
	RidepoolTypes []RidepoolCount     `json:"ridepool_types"`
	Timeline      []TimelinePoint     `json:"timeline"`
}

func ratio(numerator, denominator float64) *float64 {
	if denominator == 0 {
		return nil
	}
	v := numerator / denominator
	return &v
}

func value(v float64) *float64 { return &v }

// nearestRank is the nearest-rank percentile; values need not be sorted.
func nearestRank(values []float64, percentile float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	rank := int(math.Ceil(percentile * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	return value(sorted[rank-1])
}

func mean(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return value(sum / float64(len(values)))
}

func maximum(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	m := values[0]
	for _, v := range values {
		m = math.Max(m, v)
	}
	return value(m)
}

func histogram(values []float64, width float64) []Bucket {
	buckets := []Bucket{}
	if len(values) == 0 {
		return buckets
	}
	counts := map[int]int{}
	low, high := math.MaxInt, math.MinInt
	for _, v := range values {
		i := int(math.Floor(v/width + 1e-9))
		counts[i]++
		low, high = min(low, i), max(high, i)
	}
	for i := low; i <= high; i++ {
		buckets = append(buckets, Bucket{Lower: float64(i) * width, Upper: float64(i+1) * width, Count: counts[i]})
	}
	return buckets
}

// isForwardDispatch is true when ridepool_type has a dropoff before p0.
func isForwardDispatch(ridepoolType string) bool {
	i := strings.Index(ridepoolType, "p0")
	return i > 0 && strings.Contains(ridepoolType[:i], "d")
}

type metricBuilder struct{ metrics []Metric }

func (b *metricBuilder) add(group, anchor, name, better string, v *float64) {
	b.metrics = append(b.metrics, Metric{Name: name, Group: group, Value: v, Better: better, Definition: DefinitionsURL + "#" + anchor})
}

func (b *metricBuilder) stats(group, anchor, name, better string, values []float64, withMax bool) {
	b.add(group, anchor, name+"_mean", better, mean(values))
	b.add(group, anchor, name+"_median", better, nearestRank(values, 0.5))
	b.add(group, anchor, name+"_p90", better, nearestRank(values, 0.9))
	if withMax {
		b.add(group, anchor, name+"_max", better, maximum(values))
	}
}

// Dashboard computes every aggregate metric from the spec.
func (r *Run) Dashboard() Dashboard {
	b := &metricBuilder{}
	d := Dashboard{StartAt: r.startAt(), EndAt: r.endAt(), Distributions: map[string][]Bucket{}}

	// Supply utilization.
	var onlineSeconds, idleSeconds, enRouteSeconds, onTripSeconds, idleMeters, enRouteMeters, onTripMeters, allMeters float64
	var utilizations, maxRidersAboard []float64
	for _, supply := range r.SupplySummary {
		onlineSeconds += float64(r.onlineSeconds(supply))
		for field, sum := range map[string]*float64{"idle_seconds": &idleSeconds, "en_route_seconds": &enRouteSeconds, "on_trip_seconds": &onTripSeconds,
			"idle_meters": &idleMeters, "en_route_meters": &enRouteMeters, "on_trip_meters": &onTripMeters} {
			v, _ := integer(supply, field)
			*sum += float64(v)
		}
		allMeters += float64(totalMeters(supply))
		if u, ok := decimal(supply, "utilization"); ok {
			utilizations = append(utilizations, u)
		}
		maxRidersAboard = append(maxRidersAboard, float64(r.maxRidersAboard(supply["supply_id"])))
	}
	const utilization = "supply-utilization"
	b.add("supply_utilization", utilization, "on_trip_time_share", "higher", ratio(onTripSeconds, onlineSeconds))
	b.add("supply_utilization", utilization, "en_route_time_share", "lower", ratio(enRouteSeconds, onlineSeconds))
	b.add("supply_utilization", utilization, "idle_time_share", "", ratio(idleSeconds, onlineSeconds))
	b.add("supply_utilization", utilization, "on_trip_distance_share", "higher", ratio(onTripMeters, allMeters))
	b.add("supply_utilization", utilization, "en_route_distance_share", "lower", ratio(enRouteMeters, allMeters))
	b.add("supply_utilization", utilization, "idle_distance_share", "lower", ratio(idleMeters, allMeters))
	b.add("supply_utilization", utilization, "deadhead_distance_share", "lower", ratio(enRouteMeters+idleMeters, allMeters))
	d.Distributions["utilization"] = histogram(utilizations, 0.1)

	// Per-trip collections.
	var waits, etas, assignmentDelays, etaErrors, pickupMeters, extraMeters, detours, extraSeconds, durationRatios, totalTimeRatios, coRiders []float64
	var directMetersCompleted, tripMetersCompleted float64
	completed, canceled, canceledAssignment, canceledPickup, unserved, canceledAfterAssignment := 0, 0, 0, 0, 0, 0
	shared, forward, assignedEver, reassigned, reassignments := 0, 0, 0, 0, 0
	ridepoolCounts := map[string]int{}
	dwell := r.dropoffDwellSeconds()
	for _, trip := range r.Trips {
		requested, _ := integer(trip, "requested_at")
		if eta, ok := integer(trip, "eta_at_assignment_seconds"); ok {
			if _, assigned := integer(trip, "assigned_at"); assigned {
				etas = append(etas, float64(eta))
			}
		}
		if first, ok := integer(trip, "first_assigned_at"); ok {
			assignmentDelays = append(assignmentDelays, float64(first-requested))
			assignedEver++
			count, _ := integer(trip, "reassign_count")
			reassignments += int(count)
			if count > 0 {
				reassigned++
			}
		}
		switch trip["status"] {
		case "completed":
			completed++
			pickup, _ := integer(trip, "pickup_at")
			dropoff, _ := integer(trip, "dropoff_at")
			assignedAt, _ := integer(trip, "assigned_at")
			eta, _ := integer(trip, "eta_at_assignment_seconds")
			arrived, _ := integer(trip, "driver_arrived_at")
			tripMeters, _ := integer(trip, "trip_distance_meters")
			directMeters, _ := integer(trip, "direct_distance_meters")
			duration, _ := integer(trip, "trip_duration_seconds")
			directSeconds, _ := integer(trip, "direct_duration_seconds")
			pickupDistance, _ := integer(trip, "pickup_distance_meters")
			waits = append(waits, float64(pickup-requested))
			etaErrors = append(etaErrors, float64(arrived-(assignedAt+eta)))
			pickupMeters = append(pickupMeters, float64(pickupDistance))
			directMetersCompleted += float64(directMeters)
			tripMetersCompleted += float64(tripMeters)
			extraMeters = append(extraMeters, float64(tripMeters-directMeters))
			if detour, ok := decimal(trip, "detour_ratio"); ok {
				detours = append(detours, detour)
			}
			extraSeconds = append(extraSeconds, float64(duration)-float64(directSeconds)-dwell)
			if directSeconds > 0 {
				durationRatios = append(durationRatios, (float64(duration)-dwell)/float64(directSeconds))
				totalTimeRatios = append(totalTimeRatios, (float64(dropoff-requested)-dwell)/float64(directSeconds))
			}
			co, _ := integer(trip, "max_co_riders")
			coRiders = append(coRiders, float64(co))
			if co > 0 {
				shared++
			}
			if isForwardDispatch(trip["ridepool_type"]) {
				forward++
			}
			ridepoolCounts[trip["ridepool_type"]]++
		case "canceled":
			canceled++
			if trip["cancel_reason"] == "max_request_to_assignment" {
				canceledAssignment++
			} else {
				canceledPickup++
			}
			if trip["first_assigned_at"] != "" {
				canceledAfterAssignment++
			}
		default:
			unserved++
		}
	}

	const wait = "rider-wait"
	b.stats("rider_wait", wait, "wait_seconds", "lower", waits, true)
	b.stats("rider_wait", wait, "eta_at_assignment_seconds", "lower", etas, false)
	b.stats("rider_wait", wait, "assignment_delay_seconds", "lower", assignmentDelays, false)
	b.stats("rider_wait", wait, "eta_error_seconds", "lower", etaErrors, false)
	d.Distributions["wait_seconds"] = histogram(waits, 60)

	const efficiency = "efficiency"
	b.add("efficiency", efficiency, "rider_meters_per_supply_meter", "higher", ratio(directMetersCompleted, allMeters))
	b.add("efficiency", efficiency, "rider_meters_driven_per_supply_meter", "higher", ratio(tripMetersCompleted, allMeters))
	b.stats("efficiency", efficiency, "pickup_distance_meters", "lower", pickupMeters, false)
	b.add("efficiency", efficiency, "requests_served_per_supply_hour", "higher", ratio(float64(completed), onlineSeconds/3600))

	const completion = "completion-and-cancellations"
	all := float64(len(r.Trips))
	b.add("completion", completion, "completion_rate", "higher", ratio(float64(completed), all))
	b.add("completion", completion, "cancel_rate", "lower", ratio(float64(canceled), all))
	b.add("completion", completion, "cancel_rate_max_request_to_assignment", "lower", ratio(float64(canceledAssignment), all))
	b.add("completion", completion, "cancel_rate_max_request_to_pickup", "lower", ratio(float64(canceledPickup), all))
	b.add("completion", completion, "unserved_rate", "lower", ratio(float64(unserved), all))
	b.add("completion", completion, "cancel_after_assignment_share", "lower", ratio(float64(canceledAfterAssignment), float64(canceled)))

	const pooling = "pooling"
	b.add("pooling", pooling, "shared_trip_rate", "", ratio(float64(shared), float64(completed)))
	b.add("pooling", pooling, "forward_dispatch_rate", "", ratio(float64(forward), float64(completed)))
	d.Distributions["max_co_riders"] = histogram(coRiders, 1)
	d.Distributions["max_riders_aboard"] = histogram(maxRidersAboard, 1)
	d.RidepoolTypes = []RidepoolCount{}
	for t, count := range ridepoolCounts {
		d.RidepoolTypes = append(d.RidepoolTypes, RidepoolCount{t, count, float64(count) / float64(completed)})
	}
	sort.Slice(d.RidepoolTypes, func(i, j int) bool {
		if d.RidepoolTypes[i].Count != d.RidepoolTypes[j].Count {
			return d.RidepoolTypes[i].Count > d.RidepoolTypes[j].Count
		}
		return d.RidepoolTypes[i].RidepoolType < d.RidepoolTypes[j].RidepoolType
	})

	const inconvenience = "rider-inconvenience"
	b.stats("rider_inconvenience", inconvenience, "extra_meters", "lower", extraMeters, false)
	b.stats("rider_inconvenience", inconvenience, "detour_ratio", "lower", detours, false)
	b.stats("rider_inconvenience", inconvenience, "extra_seconds", "lower", extraSeconds, false)
	b.stats("rider_inconvenience", inconvenience, "duration_ratio", "lower", durationRatios, false)
	b.stats("rider_inconvenience", inconvenience, "total_time_ratio", "lower", totalTimeRatios, false)
	d.Distributions["detour_ratio"] = histogram(detours, 0.1)

	const other = "other"
	b.add("other", other, "reassignment_rate", "", ratio(float64(reassigned), float64(assignedEver)))
	b.add("other", other, "reassignments_per_trip", "", ratio(float64(reassignments), float64(assignedEver)))
	rejected := 0
	for _, event := range r.Events {
		if event["action"] == "plan_rejected" {
			rejected++
		}
	}
	b.add("other", other, "plan_rejected_count", "lower", value(float64(rejected)))
	for _, trigger := range []string{"request", "supply_online", "supply_offline", "batch_tick"} {
		calls, _ := decimal(r.RunSummary, "matcher_calls_"+trigger)
		b.add("other", other, "matcher_calls_"+trigger, "", value(calls))
	}
	d.Metrics = b.metrics
	d.Timeline = r.timeline(60)
	return d
}

// timeline samples open requests and cars by state every bucketSeconds.
func (r *Run) timeline(bucketSeconds int64) []TimelinePoint {
	points := []TimelinePoint{}
	for t := r.startAt(); t <= r.endAt(); t += bucketSeconds {
		point := TimelinePoint{At: t, CarsByState: map[string]int{}}
		for _, trip := range r.Trips {
			requested, _ := integer(trip, "requested_at")
			if requested > t {
				continue
			}
			closed := int64(math.MaxInt64)
			if v, ok := integer(trip, "pickup_at"); ok {
				closed = v
			} else if v, ok := integer(trip, "canceled_at"); ok {
				closed = v
			}
			if t < closed {
				point.OpenRequests++
			}
		}
		for _, id := range r.supplyIDs {
			state := "offline"
			for _, i := range r.eventsBySupply[id] {
				if at, _ := integer(r.Events[i], "at"); at <= t {
					state = r.Events[i]["state_after"]
				}
			}
			point.CarsByState[state]++
		}
		points = append(points, point)
	}
	return points
}

// metricMap indexes metrics by name.
func metricMap(metrics []Metric) map[string]Metric {
	out := map[string]Metric{}
	for _, m := range metrics {
		out[m.Name] = m
	}
	return out
}
