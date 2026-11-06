package sweep

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Grid lists the arm values. Every combination runs once per benchmark
// folder; the folders are the replicates.
type Grid struct {
	FleetFractions   []float64
	Reassignment     []bool
	MaxCandidateCars int // written to [matching] max_candidate_cars in every run
	BaseSeed         int64
}

// Config names the inputs and where outputs go. Each benchmark folder holds
// requests.csv and supply.csv, for example one prebuilt folder per seed.
type Config struct {
	SettingsPath string
	Benchmarks   []string
	OutputDir    string
	Commit       string
	Grid         Grid
}

// Columns of results.csv, in order.
var Columns = []string{
	"run_id", "commit", "settings_sha256", "simulator_settings_sha256", "benchmark", "requests_sha256", "supply_sha256",
	"seed", "replicate", "fleet_fraction", "reassignment_enabled", "max_candidate_cars",
	"supply_shifts", "requests", "completed", "canceled_after_assignment", "never_assigned", "unresolved_at_end",
	"median_wait_seconds", "p90_wait_seconds", "runtime_seconds",
}

// Run executes the grid and writes OutputDir/results.csv. It returns the rows.
func Run(config Config) ([][]string, error) {
	grid := config.Grid
	base := map[string]any{}
	if _, err := toml.DecodeFile(config.SettingsPath, &base); err != nil {
		return nil, err
	}
	rows := [][]string{Columns}
	run := 0
	for _, fraction := range grid.FleetFractions {
		for _, reassignment := range grid.Reassignment {
			for replicate, folder := range config.Benchmarks {
				run++
				seed := grid.BaseSeed + int64(replicate)
				row, err := runOne(config, base, run, folder, seed, replicate, fraction, reassignment, grid.MaxCandidateCars)
				if err != nil {
					return nil, fmt.Errorf("run %d: %w", run, err)
				}
				rows = append(rows, row)
			}
		}
	}
	file, err := os.Create(filepath.Join(config.OutputDir, "results.csv"))
	if err != nil {
		return nil, err
	}
	writer := csv.NewWriter(file)
	if err := writer.WriteAll(rows); err != nil {
		file.Close()
		return nil, err
	}
	return rows, file.Close()
}

func runOne(config Config, base map[string]any, run int, folder string, seed int64, replicate int, fraction float64, reassignment bool, maxCandidates int) ([]string, error) {
	directory := filepath.Join(config.OutputDir, fmt.Sprintf("run_%03d", run))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	supplyPath := filepath.Join(directory, "supply.csv")
	requestsPath := filepath.Join(folder, "requests.csv")
	shifts, err := ScaleSupply(filepath.Join(folder, "supply.csv"), supplyPath, fraction, seed)
	if err != nil {
		return nil, err
	}
	settingsPath := filepath.Join(directory, "settings.toml")
	if err := writeSettings(base, settingsPath, reassignment, maxCandidates); err != nil {
		return nil, err
	}
	started := time.Now()
	_, err = runner.RunFiles(settingsPath, runner.Paths{Requests: requestsPath, Supply: supplyPath, OutputDirectory: directory})
	if err != nil {
		return nil, err
	}
	runtime := time.Since(started).Seconds()
	trips, err := readTable(filepath.Join(directory, runner.TripsFile))
	if err != nil {
		return nil, err
	}
	completed, canceled, never, unresolved := 0, 0, 0, 0
	for _, trip := range trips {
		switch {
		case trip["status"] == "completed":
			completed++
		case trip["status"] != "canceled":
			unresolved++
		case trip["first_assigned_at"] == "":
			never++
		default:
			canceled++
		}
	}
	summary, err := readTable(filepath.Join(directory, runner.RunSummaryFile))
	if err != nil || len(summary) != 1 {
		return nil, fmt.Errorf("reading run summary: %v", err)
	}
	metadata, err := readTable(filepath.Join(directory, runner.RunMetadataFile))
	if err != nil || len(metadata) != 1 {
		return nil, fmt.Errorf("reading run metadata: %v", err)
	}
	requestsHash, err := fileHash(requestsPath)
	if err != nil {
		return nil, err
	}
	supplyHash, err := fileHash(filepath.Join(folder, "supply.csv"))
	if err != nil {
		return nil, err
	}
	fractionText := strconv.FormatFloat(fraction, 'f', -1, 64)
	seedText := strconv.FormatInt(seed, 10)
	// The effective settings: the simulator's settings after overrides plus
	// every sweep parameter and input, so each run's hash is unique.
	effective := sha256.Sum256([]byte(strings.Join([]string{
		metadata[0]["settings"], "fleet_fraction=" + fractionText, "seed=" + seedText,
		"requests_sha256=" + requestsHash, "supply_sha256=" + supplyHash,
	}, "\n")))
	return []string{
		strconv.Itoa(run), config.Commit, hex.EncodeToString(effective[:]), metadata[0]["settings_sha256"], folder, requestsHash, supplyHash,
		seedText, strconv.Itoa(replicate), fractionText, strconv.FormatBool(reassignment), strconv.Itoa(maxCandidates),
		strconv.Itoa(shifts), strconv.Itoa(len(trips)), strconv.Itoa(completed), strconv.Itoa(canceled), strconv.Itoa(never), strconv.Itoa(unresolved),
		summary[0]["median_wait_seconds"], summary[0]["p90_wait_seconds"], strconv.FormatFloat(runtime, 'f', 2, 64),
	}, nil
}

func writeSettings(base map[string]any, path string, reassignment bool, maxCandidates int) error {
	settings := map[string]any{}
	for key, value := range base {
		settings[key] = value
	}
	simulation := map[string]any{}
	if existing, ok := base["simulation"].(map[string]any); ok {
		for key, value := range existing {
			simulation[key] = value
		}
	}
	simulation["reassignment_enabled"] = reassignment
	settings["simulation"] = simulation
	matching := map[string]any{}
	if existing, ok := base["matching"].(map[string]any); ok {
		for key, value := range existing {
			matching[key] = value
		}
	}
	matching["max_candidate_cars"] = maxCandidates
	settings["matching"] = matching
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(file).Encode(settings); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func fileHash(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func readTable(path string) ([]map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	var table []map[string]string
	for _, row := range rows[1:] {
		record := map[string]string{}
		for i, column := range rows[0] {
			record[column] = row[i]
		}
		table = append(table, record)
	}
	return table, nil
}
