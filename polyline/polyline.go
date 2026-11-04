// Package polyline encodes points in the Google encoded polyline format (precision 5).
package polyline

import (
	"math"
	"strings"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
)

// Encode returns the encoded polyline string for points.
func Encode(points []model.Location) string {
	var b strings.Builder
	var lastLat, lastLng int64
	for _, p := range points {
		lat := int64(math.Round(p.Lat * 1e5))
		lng := int64(math.Round(p.Lng * 1e5))
		encodeValue(&b, lat-lastLat)
		encodeValue(&b, lng-lastLng)
		lastLat, lastLng = lat, lng
	}
	return b.String()
}

func encodeValue(b *strings.Builder, v int64) {
	v <<= 1
	if v < 0 {
		v = ^v
	}
	for v >= 0x20 {
		b.WriteByte(byte((0x20 | (v & 0x1f)) + 63))
		v >>= 5
	}
	b.WriteByte(byte(v + 63))
}

// Decode parses an encoded polyline string (precision 5).
func Decode(encoded string) []model.Location {
	var points []model.Location
	var lat, lng int64
	for i := 0; i < len(encoded); {
		var deltas [2]int64
		for k := 0; k < 2; k++ {
			var result int64
			shift := uint(0)
			for i < len(encoded) {
				b := int64(encoded[i]) - 63
				i++
				result |= (b & 0x1f) << shift
				shift += 5
				if b < 0x20 {
					break
				}
			}
			if result&1 != 0 {
				deltas[k] = ^(result >> 1)
			} else {
				deltas[k] = result >> 1
			}
		}
		lat += deltas[0]
		lng += deltas[1]
		points = append(points, model.Location{Lat: float64(lat) / 1e5, Lng: float64(lng) / 1e5})
	}
	return points
}
