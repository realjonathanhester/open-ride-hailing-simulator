package tests

import (
	"strconv"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/plugins/csvio"
)

// Benchmark scenario files parse with the CSV readers. See docs/benchmark_spec.md.
func TestAustinWeekdayBenchmarkParses(t *testing.T) {
	requests, err := csvio.RequestReader{Path: "../benchmarks/austin_weekday_10k/requests.csv"}.ReadRequests()
	if err != nil {
		t.Fatal(err)
	}
	supply, err := csvio.SupplyReader{Path: "../benchmarks/austin_weekday_10k/supply.csv"}.ReadSupply()
	if err != nil {
		t.Fatal(err)
	}
	assertEquals(t, strconv.Itoa(len(requests)), "10000")
	assertEquals(t, strconv.Itoa(len(supply)), "3377")
}
