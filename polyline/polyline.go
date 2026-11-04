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
