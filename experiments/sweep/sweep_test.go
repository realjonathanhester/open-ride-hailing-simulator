package sweep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const benchmark = "../../benchmarks/austin_weekday_100"

func scaled(t *testing.T, fraction float64, seed int64) (string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "supply.csv")
	count, err := ScaleSupply(filepath.Join(benchmark, "supply.csv"), path, fraction, seed)
	if err != nil {
		t.Fatalf("scale supply: %v", err)
	}
	content, _ := os.ReadFile(path)
	return string(content), count
}

func TestFullFleetIsSupplyAsBuilt(t *testing.T) {
	original, _ := os.ReadFile(filepath.Join(benchmark, "supply.csv"))
	content, count := scaled(t, 1, 7)
	assertEqual(t, "shifts", count, 143)
	assertEqual(t, "content", content, string(original))
}

func TestThinningIsReproducibleForASeed(t *testing.T) {
	first, count := scaled(t, 0.7, 20161001)
	second, _ := scaled(t, 0.7, 20161001)
	other, _ := scaled(t, 0.7, 20161002)
	assertEqual(t, "shifts at 70%", count, 98)
	assertEqual(t, "same seed gives same file", first == second, true)
	assertEqual(t, "other seed gives other file", first == other, false)
}

func TestReplicatingCopiesWholeShiftsWithUniqueIds(t *testing.T) {
	content, count := scaled(t, 2, 1)
	assertEqual(t, "shifts at 200%", count, 286)
	assertEqual(t, "copies", strings.Count(content, "_copy1,"), 143)
}

func runGrid(t *testing.T) ([][]string, string) {
	t.Helper()
	directory := t.TempDir()
	rows, err := Run(Config{
		SettingsPath: filepath.Join(benchmark, "settings.toml"),
		Benchmarks:   []string{benchmark, benchmark},
		OutputDir:    directory,
		Commit:       "abc1234",
		Grid: Grid{
			FleetFractions:   []float64{0.7, 1},
			Reassignment:     []bool{true, false},
			MaxCandidateCars: 10,
			BaseSeed:         20161001,
		},
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	return rows, directory
}

func TestGridWritesOneTidyRowPerRun(t *testing.T) {
	rows, directory := runGrid(t)
	written, _ := os.ReadFile(filepath.Join(directory, "results.csv"))
	full := record(rows, 5) // fleet 1, reassignment on, first folder
	assertEqual(t, "rows", len(rows), 9)
	assertEqual(t, "header", strings.Join(rows[0], ","), strings.Join(Columns, ","))
	assertEqual(t, "results.csv lines", strings.Count(string(written), "\n"), 9)
	assertEqual(t, "commit", full["commit"], "abc1234")
	assertEqual(t, "fleet", full["fleet_fraction"], "1")
	assertEqual(t, "seed", full["seed"], "20161001")
	assertEqual(t, "max candidate cars", full["max_candidate_cars"], "10")
	assertEqual(t, "completed", full["completed"], "100")
	assertEqual(t, "never assigned", full["never_assigned"], "0")
	assertEqual(t, "unresolved at end", full["unresolved_at_end"], "0")
	assertEqual(t, "median wait", full["median_wait_seconds"], "124")
}

func TestGridRecordsInputHashesPerFolder(t *testing.T) {
	rows, _ := runGrid(t)
	run := record(rows, 1)
	assertEqual(t, "requests hash length", len(run["requests_sha256"]), 64)
	assertEqual(t, "supply hash length", len(run["supply_sha256"]), 64)
	assertEqual(t, "benchmark folder", run["benchmark"], benchmark)
}

func TestEverySettingsHashIsUnique(t *testing.T) {
	rows, _ := runGrid(t)
	seen := map[string]bool{}
	for index := 1; index < len(rows); index++ {
		seen[record(rows, index)["settings_sha256"]] = true
	}
	assertEqual(t, "distinct settings hashes", len(seen), 8)
}

func record(rows [][]string, index int) map[string]string {
	out := map[string]string{}
	for i, column := range rows[0] {
		out[column] = rows[index][i]
	}
	return out
}


func TestWriteSettingsPreservesMatchingKeys(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.toml")
	content := strings.Join([]string{
		"[simulation]",
		"pooling_enabled = true",
		"reassignment_enabled = false",
		"",
		"[hooks]",
		`matching = "transfermatcher"`,
		`routing = "straight_line"`,
		"",
		"[matching]",
		"transfers_enabled = true",
		"max_rider_path_ratio = 1.25",
		"max_insertion_evaluations = 5000",
		"max_candidate_cars = 99",
		"",
	}, "\n")
	if err := os.WriteFile(source, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	base := map[string]any{}
	if _, err := toml.DecodeFile(source, &base); err != nil {
		t.Fatalf("decode source: %v", err)
	}
	out := filepath.Join(directory, "settings.toml")
	if err := writeSettings(base, out, true, 10); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	written := map[string]any{}
	if _, err := toml.DecodeFile(out, &written); err != nil {
		t.Fatalf("decode written: %v", err)
	}
	matching, ok := written["matching"].(map[string]any)
	if !ok {
		t.Fatalf("matching section type = %T, want map", written["matching"])
	}
	simulation := written["simulation"].(map[string]any)
	assertEqual(t, "reassignment override", simulation["reassignment_enabled"].(bool), true)
	assertEqual(t, "transfers_enabled kept", matching["transfers_enabled"].(bool), true)
	assertEqual(t, "max_rider_path_ratio kept", matching["max_rider_path_ratio"].(float64), 1.25)
	assertEqual(t, "max_insertion_evaluations kept", matching["max_insertion_evaluations"].(int64), int64(5000))
	assertEqual(t, "max_candidate_cars overridden", matching["max_candidate_cars"].(int64), int64(10))
	assertEqual(t, "matching key count", len(matching), 4)
}

func TestWriteSettingsControlMatchingOnlyAddsCandidateCars(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.toml")
	content := strings.Join([]string{
		"[simulation]",
		"pooling_enabled = true",
		"reassignment_enabled = true",
		"",
		"[hooks]",
		`matching = "greedy_nearest_eta"`,
		`routing = "straight_line"`,
		"",
	}, "\n")
	if err := os.WriteFile(source, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	base := map[string]any{}
	if _, err := toml.DecodeFile(source, &base); err != nil {
		t.Fatalf("decode source: %v", err)
	}
	out := filepath.Join(directory, "settings.toml")
	if err := writeSettings(base, out, false, 10); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	written := map[string]any{}
	if _, err := toml.DecodeFile(out, &written); err != nil {
		t.Fatalf("decode written: %v", err)
	}
	matching, ok := written["matching"].(map[string]any)
	if !ok {
		t.Fatalf("matching section type = %T, want map", written["matching"])
	}
	simulation := written["simulation"].(map[string]any)
	hooks := written["hooks"].(map[string]any)
	assertEqual(t, "control matching key count", len(matching), 1)
	assertEqual(t, "control max_candidate_cars", matching["max_candidate_cars"].(int64), int64(10))
	_, hasTransfers := matching["transfers_enabled"]
	_, hasRatio := matching["max_rider_path_ratio"]
	_, hasBudget := matching["max_insertion_evaluations"]
	assertEqual(t, "control has no transfers_enabled", hasTransfers, false)
	assertEqual(t, "control has no max_rider_path_ratio", hasRatio, false)
	assertEqual(t, "control has no max_insertion_evaluations", hasBudget, false)
	assertEqual(t, "control reassignment override", simulation["reassignment_enabled"].(bool), false)
	assertEqual(t, "control matcher hook", hooks["matching"].(string), "greedy_nearest_eta")
	assertEqual(t, "control routing hook", hooks["routing"].(string), "straight_line")
}

func assertEqual[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}
