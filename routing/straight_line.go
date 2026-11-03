// Package routing holds the built-in straight-line router.
package routing

import (
	"fmt"
	"math"

	"github.com/realjonathanhester/open-ride-hailing-simulator/model"
)

// EarthRadiusMeters is the mean Earth radius.
const EarthRadiusMeters = 6371008.8

// StraightLine routes along literal straight lines at a constant speed.
type StraightLine struct {
	AverageSpeedMetersPerSecond float64
}

// NewStraightLine returns a straight-line router.
func NewStraightLine(averageSpeedMetersPerSecond float64) (*StraightLine, error) {
	if averageSpeedMetersPerSecond <= 0 {
		return nil, fmt.Errorf("average_speed_meters_per_second must be positive")
	}
	return &StraightLine{AverageSpeedMetersPerSecond: averageSpeedMetersPerSecond}, nil
}

// HaversineMeters returns the great-circle distance rounded to whole meters.
func HaversineMeters(a, b model.Location) int64 {
	toRadians := math.Pi / 180
	dLat := (b.Lat - a.Lat) * toRadians
	dLng := (b.Lng - a.Lng) * toRadians
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*toRadians)*math.Cos(b.Lat*toRadians)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return int64(math.Round(2 * EarthRadiusMeters * math.Asin(math.Min(1, math.Sqrt(h)))))
}

// Route returns one single-segment leg per consecutive pair of points.
func (r *StraightLine) Route(start model.Location, waypoints []model.Location, departAt int64) ([]model.Leg, error) {
	legs := make([]model.Leg, 0, len(waypoints))
	from := start
	for _, to := range waypoints {
		distance := HaversineMeters(from, to)
		duration := int64(math.Round(float64(distance) / r.AverageSpeedMetersPerSecond))
		segment := model.Segment{Polyline: []model.Location{from, to}, DistanceMeters: distance, DurationSeconds: duration}
		legs = append(legs, model.Leg{Segments: []model.Segment{segment}, DistanceMeters: distance, DurationSeconds: duration})
		from = to
	}
	return legs, nil
}
