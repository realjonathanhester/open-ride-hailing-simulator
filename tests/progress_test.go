package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Live progress while rhs run executes. Progress writes to stderr (or a
// test buffer) only and must leave every output file byte-identical.

func TestProgressDoesNotChangeOutputs(t *testing.T) {
	without := simulate(t, "base_default", "one_car_at_union_square", "single_trip")
	directory := t.TempDir()
	var buf bytes.Buffer
	_, err := runner.RunWithOptions(filepath.Join("fixtures", "settings", "base_default.toml"), runner.Paths{
		Supply:          filepath.Join("fixtures", "supply", "one_car_at_union_square.csv"),
		Requests:        filepath.Join("fixtures", "demand", "single_trip.csv"),
		OutputDirectory: directory,
	}, runner.Options{Progress: &runner.ProgressOptions{Writer: &buf, Terminal: false, Interval: time.Nanosecond}})
	if err != nil {
		t.Fatalf("simulation failed: %v", err)
	}
	with := readOutputs(t, directory)
	assertFile(t, "trips", with.files["trips.csv"], without.files["trips.csv"])
	assertFile(t, "supply event log", with.files["supply_event_log.csv"], without.files["supply_event_log.csv"])
	assertFile(t, "supply summary", with.files["supply_summary.csv"], without.files["supply_summary.csv"])
	assertFile(t, "run summary", with.runSummaryWithoutTimings(), without.runSummaryWithoutTimings())
	assertFile(t, "run metadata", with.files["run_metadata.csv"], without.files["run_metadata.csv"])
	if !strings.Contains(buf.String(), "drivers") {
		t.Fatalf("progress wrote no driver counts: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "requests") {
		t.Fatalf("progress wrote no request counts: %q", buf.String())
	}
}

func TestProgressLineReportsCompletedTrip(t *testing.T) {
	directory := t.TempDir()
	var buf bytes.Buffer
	_, err := runner.RunWithOptions(filepath.Join("fixtures", "settings", "base_default.toml"), runner.Paths{
		Supply:          filepath.Join("fixtures", "supply", "one_car_at_kezar_stadium.csv"),
		Requests:        filepath.Join("fixtures", "demand", "single_trip.csv"),
		OutputDirectory: directory,
	}, runner.Options{Progress: &runner.ProgressOptions{Writer: &buf, Terminal: false, Interval: time.Nanosecond}})
	if err != nil {
		t.Fatalf("simulation failed: %v", err)
	}
	text := buf.String()
	assertContainsProgress(t, text, "completed")
	assertContainsProgress(t, text, "%")
	assertContainsProgress(t, text, "T+")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	last := lines[len(lines)-1]
	assertContainsProgress(t, last, "1 completed")
}

func readOutputs(t *testing.T, directory string) outputs {
	t.Helper()
	o := outputs{files: map[string]string{}, t: t}
	for _, name := range []string{runner.TripsFile, runner.SupplyEventsFile, runner.SupplySummaryFile, runner.RunSummaryFile, runner.RunMetadataFile} {
		content, err := readFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		o.files[name] = content
	}
	return o
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func assertContainsProgress(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to contain %q", got, want)
	}
}
