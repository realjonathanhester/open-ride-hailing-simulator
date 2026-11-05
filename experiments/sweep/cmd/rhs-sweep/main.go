// Command rhs-sweep runs a settings grid over one benchmark. See
// experiments/sweep/README.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/realjonathanhester/ride-hailing-simulator/experiments/sweep"
)

func main() {
	settings := flag.String("settings", "benchmarks/austin_weekday_100/settings.toml", "base settings file")
	benchmarks := flag.String("benchmarks", "benchmarks/austin_weekday_100", "comma-separated benchmark folders with requests.csv and supply.csv, one per replicate")
	output := flag.String("output", "out/sweep", "output directory")
	fleet := flag.String("fleet", "0.7,1", "comma-separated fleet fractions (1 is supply.csv as built)")
	reassignment := flag.String("reassignment", "true,false", "comma-separated reassignment_enabled values")
	seed := flag.Int64("seed", 20161001, "base seed for the supply scaler; folder r uses seed+r")
	maxCandidates := flag.Int("max-candidate-cars", 10, "[matching] max_candidate_cars for every run (0 is no limit)")
	flag.Parse()

	grid := sweep.Grid{MaxCandidateCars: *maxCandidates, BaseSeed: *seed}
	var err error
	if grid.FleetFractions, err = parseList(*fleet, func(s string) (float64, error) { return strconv.ParseFloat(s, 64) }); err == nil {
		grid.Reassignment, err = parseList(*reassignment, strconv.ParseBool)
	}
	if err != nil {
		fail(err)
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		fail(err)
	}
	commit, _ := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	started := time.Now()
	rows, err := sweep.Run(sweep.Config{
		SettingsPath: *settings,
		Benchmarks:   strings.Split(*benchmarks, ","),
		OutputDir:    *output,
		Commit:       strings.TrimSpace(string(commit)),
		Grid:         grid,
	})
	if err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "%d runs in %s, results in %s\n", len(rows)-1, time.Since(started).Round(time.Millisecond), filepath.Join(*output, "results.csv"))
}

func parseList[T any](text string, parse func(string) (T, error)) ([]T, error) {
	var values []T
	for _, part := range strings.Split(text, ",") {
		value, err := parse(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("bad list value %q: %w", part, err)
		}
		values = append(values, value)
	}
	return values, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "rhs-sweep:", err)
	os.Exit(1)
}
