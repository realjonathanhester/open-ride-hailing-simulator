// Package viewer serves the local results viewer described in docs/tooling_spec.md.
package viewer

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// Row is one output row keyed by column name.
type Row map[string]string

// Table is one output table.
type Table []Row

// Output table names.
const (
	TableTrips         = "trips"
	TableSupplyEvents  = "supply_event_log"
	TableSupplySummary = "supply_summary"
	TableRunSummary    = "run_summary"
	TableRunMetadata   = "run_metadata"
)

// TableReader reads one output table by name. CSV is the default; other
// formats plug in here, like the simulator's reader plugins.
type TableReader interface {
	ReadTable(name string) (Table, error)
}

// CSVTableReader reads output tables from an output directory.
type CSVTableReader struct{ Directory string }

var csvFiles = map[string]string{
	TableTrips: runner.TripsFile, TableSupplyEvents: runner.SupplyEventsFile, TableSupplySummary: runner.SupplySummaryFile,
	TableRunSummary: runner.RunSummaryFile, TableRunMetadata: runner.RunMetadataFile,
}

// ReadTable implements TableReader.
func (r CSVTableReader) ReadTable(name string) (Table, error) {
	path := filepath.Join(r.Directory, csvFiles[name])
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%s: missing header row", path)
	}
	var table Table
	for _, record := range records[1:] {
		row := Row{}
		for i, column := range records[0] {
			if i < len(record) {
				row[column] = record[i]
			}
		}
		table = append(table, row)
	}
	return table, nil
}

// Run is one loaded simulation run.
type Run struct {
	Trips         Table
	Events        Table
	SupplySummary Table
	RunSummary    Row
	Metadata      Row
	Settings      map[string]any

	tripByID       map[string]Row
	eventsBySupply map[string][]int
	supplyByID     map[string]Row
	supplyIDs      []string
}

// LoadRun reads every output table and indexes it.
func LoadRun(reader TableReader) (*Run, error) {
	run := &Run{}
	var err error
	if run.Trips, err = reader.ReadTable(TableTrips); err != nil {
		return nil, err
	}
	if run.Events, err = reader.ReadTable(TableSupplyEvents); err != nil {
		return nil, err
	}
	if run.SupplySummary, err = reader.ReadTable(TableSupplySummary); err != nil {
		return nil, err
	}
	summary, err := reader.ReadTable(TableRunSummary)
	if err != nil {
		return nil, err
	}
	metadata, err := reader.ReadTable(TableRunMetadata)
	if err != nil {
		return nil, err
	}
	if len(summary) != 1 || len(metadata) != 1 {
		return nil, fmt.Errorf("run summary and run metadata must have exactly one row")
	}
	run.RunSummary, run.Metadata = summary[0], metadata[0]
	if err := json.Unmarshal([]byte(run.Metadata["settings"]), &run.Settings); err != nil {
		return nil, fmt.Errorf("run metadata settings: %w", err)
	}
	run.tripByID = map[string]Row{}
	for _, trip := range run.Trips {
		run.tripByID[trip["request_id"]] = trip
	}
	run.eventsBySupply = map[string][]int{}
	for i, event := range run.Events {
		run.eventsBySupply[event["supply_id"]] = append(run.eventsBySupply[event["supply_id"]], i)
	}
	run.supplyByID = map[string]Row{}
	for _, supply := range run.SupplySummary {
		run.supplyByID[supply["supply_id"]] = supply
		run.supplyIDs = append(run.supplyIDs, supply["supply_id"])
	}
	sort.Strings(run.supplyIDs)
	return run, nil
}

// integer parses an integer cell; ok is false when the cell is empty or invalid.
func integer(row Row, field string) (int64, bool) {
	v, err := strconv.ParseInt(row[field], 10, 64)
	return v, err == nil
}

func decimal(row Row, field string) (float64, bool) {
	v, err := strconv.ParseFloat(row[field], 64)
	return v, err == nil
}

func (r *Run) startAt() int64 { v, _ := integer(r.Metadata, "simulation_start_at"); return v }
func (r *Run) endAt() int64   { v, _ := integer(r.Metadata, "simulation_end_at"); return v }

func (r *Run) dropoffDwellSeconds() float64 {
	v, _ := r.Settings["dropoff_dwell_seconds"].(float64)
	return v
}

// onlineSeconds is (offline_at or run end) - online_at.
func (r *Run) onlineSeconds(supply Row) int64 {
	online, ok := integer(supply, "online_at")
	if !ok {
		return 0
	}
	end := r.endAt()
	if offline, ok := integer(supply, "offline_at"); ok {
		end = offline
	}
	return end - online
}

func totalMeters(supply Row) int64 {
	idle, _ := integer(supply, "idle_meters")
	enRoute, _ := integer(supply, "en_route_meters")
	onTrip, _ := integer(supply, "on_trip_meters")
	return idle + enRoute + onTrip
}

// maxRidersAboard counts pickups minus dropoffs along one car's event log.
func (r *Run) maxRidersAboard(supplyID string) int {
	aboard, most := 0, 0
	for _, i := range r.eventsBySupply[supplyID] {
		switch r.Events[i]["action"] {
		case "pickup":
			aboard++
		case "dropoff":
			aboard--
		}
		if aboard > most {
			most = aboard
		}
	}
	return most
}
