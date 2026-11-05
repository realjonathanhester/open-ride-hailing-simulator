// Package sweep runs a grid of simulator settings over one benchmark and
// writes one tidy results row per run. It lives outside the core and uses
// only the public runner API.
package sweep

import (
	"encoding/csv"
	"fmt"
	"math"
	"math/rand"
	"os"
)

// ScaleSupply writes a copy of a supply CSV with about fraction times as
// many shifts. Whole shifts are kept, dropped or copied: each shift gets
// floor(fraction) copies plus one more with probability equal to the
// remainder. Fraction 1 copies the file unchanged. Copies get a suffix
// "_copyN" on supply_id so ids stay unique.
func ScaleSupply(inPath, outPath string, fraction float64, seed int64) (int, error) {
	if fraction <= 0 {
		return 0, fmt.Errorf("fleet fraction must be above 0, got %v", fraction)
	}
	in, err := os.Open(inPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	rows, err := csv.NewReader(in).ReadAll()
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 || rows[0][0] != "supply_id" {
		return 0, fmt.Errorf("%s: first column must be supply_id", inPath)
	}
	out := [][]string{rows[0]}
	random := rand.New(rand.NewSource(seed))
	whole, remainder := math.Floor(fraction), fraction-math.Floor(fraction)
	for _, row := range rows[1:] {
		copies := int(whole)
		if fraction != 1 && random.Float64() < remainder {
			copies++
		}
		for c := 0; c < copies; c++ {
			copied := append([]string(nil), row...)
			if c > 0 {
				copied[0] = fmt.Sprintf("%s_copy%d", row[0], c)
			}
			out = append(out, copied)
		}
	}
	file, err := os.Create(outPath)
	if err != nil {
		return 0, err
	}
	writer := csv.NewWriter(file)
	if err := writer.WriteAll(out); err != nil {
		file.Close()
		return 0, err
	}
	return len(out) - 1, file.Close()
}
