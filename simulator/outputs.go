package simulator

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/polyline"
)

// Output column orders, exactly as in docs/spec.md.
var (
	TripColumns = []string{"request_id", "status", "requested_at", "num_requested_seats", "first_assigned_at", "assigned_at", "supply_id",
		"reassign_count", "ridepool_type", "eta_at_assignment_seconds", "driver_arrived_at", "pickup_at", "dropoff_at", "wait_seconds",
		"trip_distance_meters", "trip_duration_seconds", "direct_distance_meters", "direct_duration_seconds", "detour_ratio", "pickup_distance_meters",
		"max_co_riders", "canceled_at", "cancel_reason"}
	// TripHandoffColumns are appended to TripColumns when handoffs are enabled.
	TripHandoffColumns = []string{"handoff_count", "total_handoff_wait_seconds"}
	TripLegColumns     = []string{"request_id", "leg_index", "supply_id", "start_lat", "start_lng", "end_lat", "end_lng",
		"pickup_at", "dropoff_at", "distance_meters", "duration_seconds"}
	SupplyEventColumns = []string{"supply_id", "at", "action", "lat", "lng", "state_after", "request_id", "plan_id", "seats_in_use",
		"odometer_meters", "driven_polyline", "planned_polyline", "plan"}
	SupplySummaryColumns = []string{"supply_id", "online_at", "offline_at", "idle_seconds", "en_route_seconds", "on_trip_seconds",
		"idle_meters", "en_route_meters", "on_trip_meters", "requests_served", "utilization", "deadhead_ratio", "full_polyline"}
	RunSummaryColumns = []string{"requests_completed", "requests_canceled", "requests_canceled_max_request_to_assignment",
		"requests_canceled_max_request_to_pickup", "requests_unserved", "median_wait_seconds", "p90_wait_seconds", "mean_detour_ratio",
		"fleet_utilization", "matcher_calls_request", "matcher_calls_supply_online", "matcher_calls_supply_offline", "matcher_calls_batch_tick",
		"matcher_duration_microseconds", "router_duration_microseconds"}
	// RunSummaryHandoffColumns are appended when handoffs are enabled.
	RunSummaryHandoffColumns = []string{"requests_canceled_max_handoff_wait"}
)

// TripColumnsFor returns the trips header for these settings.
func TripColumnsFor(settings model.Settings) []string {
	if !settings.HandoffsEnabled {
		return TripColumns
	}
	return append(append([]string{}, TripColumns...), TripHandoffColumns...)
}

// RunSummaryColumnsFor returns the run summary header for these settings.
func RunSummaryColumnsFor(settings model.Settings) []string {
	if !settings.HandoffsEnabled {
		return RunSummaryColumns
	}
	return append(append([]string{}, RunSummaryColumns...), RunSummaryHandoffColumns...)
}

// RunSummaryTimingColumns are the only nondeterministic run summary columns.
var RunSummaryTimingColumns = []string{"matcher_duration_microseconds", "router_duration_microseconds"}

// Result holds every output table as rows of CSV cells.
type Result struct {
	Trips         [][]string
	TripLegs      [][]string
	SupplyEvents  [][]string
	SupplySummary [][]string
	RunSummary    [][]string
	RunMetadata   [][]string
}

type planCell struct {
	RequestID string         `json:"request_id"`
	Type      model.StopType `json:"type"`
	Lat       float64        `json:"lat"`
	Lng       float64        `json:"lng"`
	EtaAt     int64          `json:"eta_at"`
}

func formatCoordinate(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }
func formatRatio(v float64) string      { return strconv.FormatFloat(v, 'f', 6, 64) }
func formatInt(v int64) string          { return strconv.FormatInt(v, 10) }
func formatOptional(v *int64) string {
	if v == nil {
		return ""
	}
	return formatInt(*v)
}

func (s *Simulator) log(c *car, action, requestID string, planned []model.Location) {
	location, odometer, passed, _ := s.traverse(c)
	driven := ""
	// The sub-path driven since the last row: geometry points passed (and
	// reroute points) in order, then the current location.
	points := []model.Location{c.lastRowLocation}
	for _, p := range append(append(append([]model.Location{}, c.trail...), passed...), location) {
		if p != points[len(points)-1] {
			points = append(points, p)
		}
	}
	if len(points) > 1 {
		driven = polyline.Encode(points)
		c.path = append(c.path, points[1:]...)
	}
	c.trail = nil
	c.lastRowLocation = location
	plannedCell, planJSON := "", ""
	if planned != nil {
		plannedCell = polyline.Encode(planned)
	}
	if action == "plan_updated" {
		cells := []planCell{}
		for _, stop := range c.stops {
			cells = append(cells, planCell{stop.RequestID, stop.Type, stop.location.Lat, stop.location.Lng, stop.etaAt})
		}
		encoded, _ := json.Marshal(cells)
		planJSON = string(encoded)
	}
	s.eventRows = append(s.eventRows, []string{c.input.SupplyID, formatInt(s.now), action, formatCoordinate(location.Lat), formatCoordinate(location.Lng),
		c.state(), requestID, c.planID, strconv.Itoa(c.seatsInUse), formatInt(odometer), driven, plannedCell, planJSON})
}

func (s *Simulator) result() *Result {
	out := &Result{SupplyEvents: s.eventRows}
	var waits []int64
	var detourSum float64
	completed, canceledAssignment, canceledPickup, canceledHandoff, unserved := 0, 0, 0, 0, 0
	for _, id := range s.requestOrder {
		r := s.requests[id]
		row := []string{id, r.status, formatInt(r.input.RequestedAt), strconv.Itoa(r.seats), formatOptional(r.firstAssignedAt),
			formatOptional(r.assignedAt), r.supplyID, strconv.Itoa(r.reassignCount), r.ridepoolType, formatOptional(r.etaAtAssignment),
			formatOptional(r.driverArrivedAt), formatOptional(r.pickupAt), formatOptional(r.dropoffAt), "", "", "",
			formatInt(r.directDistanceMeters), formatInt(r.directDurationSeconds), "", "", "", formatOptional(r.canceledAt), r.cancelReason}
		if r.pickupAt != nil {
			wait := *r.pickupAt - r.input.RequestedAt
			row[13] = formatInt(wait)
			row[19] = formatInt(r.odometerAtPickup - r.odometerAtAssign)
			row[20] = strconv.Itoa(r.maxCoRiders)
			waits = append(waits, wait)
		}
		tripDistance := r.odometerAtDropoff - r.odometerAtPickup
		if len(r.legs) > 0 {
			tripDistance = 0
			for _, leg := range r.legs {
				tripDistance += leg.distanceMeters
			}
		}
		switch r.status {
		case model.StatusCompleted:
			completed++
			row[14] = formatInt(tripDistance)
			row[15] = formatInt(*r.dropoffAt - *r.pickupAt)
			ratio := 1.0
			if r.directDistanceMeters > 0 {
				ratio = float64(tripDistance) / float64(r.directDistanceMeters)
			}
			detourSum += ratio
			row[18] = formatRatio(ratio)
		case model.StatusCanceled:
			switch r.cancelReason {
			case "max_request_to_assignment":
				canceledAssignment++
			case "max_handoff_wait":
				canceledHandoff++
			default:
				canceledPickup++
			}
		default:
			unserved++
		}
		if s.settings.HandoffsEnabled {
			row = append(row, strconv.Itoa(r.handoffCount), formatInt(r.totalHandoffWaitSeconds))
			for i, leg := range r.legs {
				endLat, endLng := "", ""
				if leg.dropoffAt != nil {
					endLat, endLng = formatCoordinate(leg.end.Lat), formatCoordinate(leg.end.Lng)
				}
				out.TripLegs = append(out.TripLegs, []string{
					id, strconv.Itoa(i), leg.supplyID,
					formatCoordinate(leg.start.Lat), formatCoordinate(leg.start.Lng), endLat, endLng,
					formatOptional(leg.pickupAt), formatOptional(leg.dropoffAt),
					formatInt(leg.distanceMeters), formatInt(leg.durationSeconds),
				})
			}
		}
		out.Trips = append(out.Trips, row)
	}
	var onTripTotal, onlineTotal int64
	for _, id := range s.carOrder {
		c := s.cars[id]
		if !c.online {
			out.SupplySummary = append(out.SupplySummary, []string{id, "", "", "0", "0", "0", "0", "0", "0", "0", "", "", ""})
			continue
		}
		end := s.endAt
		if c.offlineAt != nil {
			end = *c.offlineAt
		}
		online := end - c.input.OnlineAt
		onTripTotal += c.classSeconds[classOnTrip]
		onlineTotal += online
		utilization, deadhead := "", ""
		if online > 0 {
			utilization = formatRatio(float64(c.classSeconds[classOnTrip]) / float64(online))
		}
		total := c.classMeters[classIdle] + c.classMeters[classEnRoute] + c.classMeters[classOnTrip]
		if total > 0 {
			deadhead = formatRatio(float64(c.classMeters[classIdle]+c.classMeters[classEnRoute]) / float64(total))
		} else {
			deadhead = formatRatio(0)
		}
		out.SupplySummary = append(out.SupplySummary, []string{id, formatInt(c.input.OnlineAt), formatOptional(c.offlineAt),
			formatInt(c.classSeconds[classIdle]), formatInt(c.classSeconds[classEnRoute]), formatInt(c.classSeconds[classOnTrip]),
			formatInt(c.classMeters[classIdle]), formatInt(c.classMeters[classEnRoute]), formatInt(c.classMeters[classOnTrip]),
			strconv.Itoa(c.served), utilization, deadhead, polyline.Encode(c.path)})
	}
	median, p90, meanDetour, fleetUtilization := "", "", "", ""
	if len(waits) > 0 {
		sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
		median, p90 = formatInt(nearestRank(waits, 0.5)), formatInt(nearestRank(waits, 0.9))
	}
	if completed > 0 {
		meanDetour = formatRatio(detourSum / float64(completed))
	}
	if onlineTotal > 0 {
		fleetUtilization = formatRatio(float64(onTripTotal) / float64(onlineTotal))
	}
	summary := []string{strconv.Itoa(completed), strconv.Itoa(canceledAssignment + canceledPickup + canceledHandoff), strconv.Itoa(canceledAssignment),
		strconv.Itoa(canceledPickup), strconv.Itoa(unserved), median, p90, meanDetour, fleetUtilization,
		strconv.Itoa(s.matcherCalls[model.TriggerRequest]), strconv.Itoa(s.matcherCalls[model.TriggerSupplyOnline]),
		strconv.Itoa(s.matcherCalls[model.TriggerSupplyOffline]), strconv.Itoa(s.matcherCalls[model.TriggerBatchTick]),
		formatInt(s.matcherElapsed.Microseconds()), formatInt(s.routerElapsed.Microseconds())}
	if s.settings.HandoffsEnabled {
		summary = append(summary, strconv.Itoa(canceledHandoff))
	}
	out.RunSummary = [][]string{summary}
	return out
}

// nearestRank is the nearest-rank percentile of sorted values.
func nearestRank(sorted []int64, percentile float64) int64 {
	rank := int(math.Ceil(percentile * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}
