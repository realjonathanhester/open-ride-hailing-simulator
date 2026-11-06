package tests

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/plugins/csvio"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// The 100 ride Austin benchmark parses and runs end to end with straight-line
// routing. See docs/benchmark_spec.md and benchmarks/austin_weekday_100/README.md.
func TestAustinWeekday100BenchmarkRuns(t *testing.T) {
	benchmark := filepath.Join("..", "benchmarks", "austin_weekday_100")
	requests, err := csvio.RequestReader{Path: filepath.Join(benchmark, "requests.csv")}.ReadRequests()
	if err != nil {
		t.Fatal(err)
	}
	supply, err := csvio.SupplyReader{Path: filepath.Join(benchmark, "supply.csv")}.ReadSupply()
	if err != nil {
		t.Fatal(err)
	}
	assertEquals(t, strconv.Itoa(len(requests)), "100")
	assertEquals(t, strconv.Itoa(len(supply)), "76")

	directory := t.TempDir()
	_, err = runner.RunFiles(filepath.Join(benchmark, "settings.toml"), runner.Paths{
		Supply:          filepath.Join(benchmark, "supply.csv"),
		Requests:        filepath.Join(benchmark, "requests.csv"),
		OutputDirectory: directory,
	})
	if err != nil {
		t.Fatalf("simulation failed: %v", err)
	}
	o := outputs{files: map[string]string{}, t: t}
	content, err := os.ReadFile(filepath.Join(directory, runner.RunSummaryFile))
	if err != nil {
		t.Fatal(err)
	}
	o.files[runner.RunSummaryFile] = string(content)
	summary := o.rows(runner.RunSummaryFile)[0]
	assertEquals(t, summary["requests_completed"], "100")
	assertEquals(t, summary["requests_canceled"], "0")
	assertEquals(t, summary["requests_unserved"], "0")
	assertEquals(t, summary["median_wait_seconds"], "254")
	assertEquals(t, summary["p90_wait_seconds"], "480")
}
