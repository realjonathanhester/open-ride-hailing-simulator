// Package runner wires settings, hooks, readers and writers into one run.
package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/realjonathanhester/ride-hailing-simulator/driverbehavior"
	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/plugins/csvio"
	"github.com/realjonathanhester/ride-hailing-simulator/plugins/tomlsettings"
	"github.com/realjonathanhester/ride-hailing-simulator/progress"
	"github.com/realjonathanhester/ride-hailing-simulator/routing"
	"github.com/realjonathanhester/ride-hailing-simulator/routing/osrm"
	"github.com/realjonathanhester/ride-hailing-simulator/simulator"
)

// Output file names written to the output directory.
const (
	TripsFile         = "trips.csv"
	SupplyEventsFile  = "supply_event_log.csv"
	SupplySummaryFile = "supply_summary.csv"
	RunSummaryFile    = "run_summary.csv"
	RunMetadataFile   = "run_metadata.csv"
)

// Paths overrides the [io] paths from the settings file when non-empty.
type Paths struct {
	Requests        string
	Supply          string
	OutputDirectory string
}

// Options controls optional run behavior that never changes simulation
// outputs, such as the live progress line on stderr.
type Options struct {
	// Progress enables the live status line. When nil, progress is off.
	Progress *ProgressOptions
}

// ProgressOptions configures the live status line.
type ProgressOptions struct {
	Writer   io.Writer // defaults to os.Stderr
	Terminal bool      // redraw in place when true
	Interval time.Duration
}

// NewMatcher returns a matching hook by name.
func NewMatcher(name string) (model.Matcher, error) {
	switch name {
	case "greedy_nearest_eta", "reference_test_matcher":
		return matching.NewGreedy(), nil
	}
	return nil, fmt.Errorf("unknown matching hook %q", name)
}

// NewDriverBehavior returns a driver behavior plugin by name; empty is simple.
func NewDriverBehavior(name string) (model.DriverBehavior, error) {
	switch name {
	case "", driverbehavior.SimpleName:
		return driverbehavior.Simple{}, nil
	case driverbehavior.ReturnToOriginName:
		return driverbehavior.ReturnToOrigin{}, nil
	}
	return nil, fmt.Errorf("unknown driver behavior plugin %q", name)
}

// NewRouter returns a routing hook by name.
func NewRouter(name string, settings model.Settings) (model.Router, error) {
	switch name {
	case "straight_line":
		return routing.NewStraightLine(settings.AverageSpeedMetersPerSecond)
	case "osrm":
		return newOSRM(settings.OSRM)
	}
	return nil, fmt.Errorf("unknown routing hook %q", name)
}

// newOSRM builds the OSRM router. Dataset fields set directly in [osrm]
// override the ones read from dataset_metadata_path.
func newOSRM(rs model.OSRMSettings) (model.Router, error) {
	var dataset osrm.Dataset
	if rs.DatasetMetadataPath != "" {
		d, err := osrm.ReadDatasetMetadata(rs.DatasetMetadataPath)
		if err != nil {
			return nil, err
		}
		dataset = d
	}
	for target, override := range map[*string]string{&dataset.Name: rs.DatasetName, &dataset.Date: rs.DatasetDate, &dataset.SHA256: rs.DatasetSHA256} {
		if override != "" {
			*target = override
		}
	}
	return osrm.New(osrm.Options{URL: rs.URL, Profile: rs.Profile, CachePath: rs.CachePath, Dataset: dataset})
}

// RunFiles reads a TOML settings file, runs the simulation and writes outputs.
// Progress is off; use RunWithOptions to enable the live status line.
func RunFiles(settingsPath string, paths Paths) (*simulator.Result, error) {
	return RunWithOptions(settingsPath, paths, Options{})
}

// RunWithOptions is RunFiles with optional live progress on a writer.
func RunWithOptions(settingsPath string, paths Paths, options Options) (*simulator.Result, error) {
	settings, err := tomlsettings.Reader{Path: settingsPath}.ReadSettings()
	if err != nil {
		return nil, err
	}
	for target, override := range map[*string]string{&settings.IO.RequestsPath: paths.Requests, &settings.IO.SupplyPath: paths.Supply, &settings.IO.OutputDirectory: paths.OutputDirectory} {
		if override != "" {
			*target = override
		}
	}
	if settings.IO.Reader != "csv" || settings.IO.Writer != "csv" {
		return nil, fmt.Errorf("only the csv reader and writer are built in")
	}
	if settings.IO.RequestsPath == "" || settings.IO.SupplyPath == "" || settings.IO.OutputDirectory == "" {
		return nil, fmt.Errorf("requests, supply and output directory paths are required")
	}
	requests, err := csvio.RequestReader{Path: settings.IO.RequestsPath}.ReadRequests()
	if err != nil {
		return nil, err
	}
	supplies, err := csvio.SupplyReader{Path: settings.IO.SupplyPath}.ReadSupply()
	if err != nil {
		return nil, err
	}
	matcher, err := NewMatcher(settings.MatchingHook)
	if err != nil {
		return nil, err
	}
	router, err := NewRouter(settings.RoutingHook, settings)
	if err != nil {
		return nil, err
	}
	if closer, ok := router.(io.Closer); ok {
		defer closer.Close()
	}
	if provider, ok := router.(model.RouterMetadataProvider); ok {
		settings.Router = provider.RouterMetadata()
	}
	behavior, err := NewDriverBehavior(settings.DriverBehaviorHook)
	if err != nil {
		return nil, err
	}
	sim := simulator.New(settings, router, matcher)
	sim.SetDriverBehavior(behavior)

	var reporter *progress.Reporter
	if options.Progress != nil {
		writer := options.Progress.Writer
		if writer == nil {
			writer = os.Stderr
		}
		var location *time.Location
		if settings.Timezone != "" {
			location, err = time.LoadLocation(settings.Timezone)
			if err != nil {
				return nil, fmt.Errorf("timezone %q: %w", settings.Timezone, err)
			}
		}
		interval := options.Progress.Interval
		if interval <= 0 {
			if options.Progress.Terminal {
				interval = 250 * time.Millisecond
			} else {
				interval = 2 * time.Second
			}
		}
		// The simulator throttles callbacks; the reporter writes every one.
		reporter = &progress.Reporter{
			Writer:   writer,
			Terminal: options.Progress.Terminal,
			Location: location,
			Interval: time.Nanosecond,
		}
		osrmStats, _ := router.(osrmCounter)
		sim.SetProgress(func(p simulator.Progress) {
			reporter.Update(toSnapshot(p, osrmStats))
		}, interval)
	}

	result, err := sim.Run(requests, supplies)
	if err != nil {
		return nil, err
	}
	if reporter != nil {
		reporter.Finish()
	}
	if counter, ok := router.(interface{ RequestCounts() (int64, int64) }); ok && options.Progress == nil {
		table, route := counter.RequestCounts()
		fmt.Fprintf(os.Stderr, "osrm: %d table requests, %d route requests\n", table, route)
	}
	if err := os.MkdirAll(settings.IO.OutputDirectory, 0o755); err != nil {
		return nil, err
	}
	outputs := []struct {
		name    string
		columns []string
		rows    [][]string
	}{
		{TripsFile, simulator.TripColumns, result.Trips},
		{SupplyEventsFile, simulator.SupplyEventColumns, result.SupplyEvents},
		{SupplySummaryFile, simulator.SupplySummaryColumns, result.SupplySummary},
		{RunSummaryFile, simulator.RunSummaryColumns, result.RunSummary},
		{RunMetadataFile, simulator.RunMetadataColumnsFor(settings), result.RunMetadata},
	}
	for _, o := range outputs {
		if err := write(&csvio.Writer{Path: filepath.Join(settings.IO.OutputDirectory, o.name)}, o.columns, o.rows); err != nil {
			return nil, err
		}
	}
	return result, nil
}

type osrmCounter interface {
	RequestCounts() (table, route int64)
	CacheHits() int64
}

func toSnapshot(p simulator.Progress, counter osrmCounter) progress.Snapshot {
	snap := progress.Snapshot{
		Now: p.Now, StartAt: p.StartAt, HorizonAt: p.HorizonAt,
		DriversOnline: p.DriversOnline, DriversIdle: p.DriversIdle,
		DriversEnRoute: p.DriversEnRoute, DriversOnTrip: p.DriversOnTrip,
		DriversRepositioning: p.DriversRepositioning,
		RequestsWaiting: p.RequestsWaiting, RequestsAssigned: p.RequestsAssigned,
		RequestsCompleted: p.RequestsCompleted, RequestsCanceled: p.RequestsCanceled,
		RequestsTotal: p.RequestsTotal,
	}
	if counter != nil {
		snap.OSRM = true
		snap.OSRMTable, snap.OSRMRoute = counter.RequestCounts()
		snap.OSRMCacheHits = counter.CacheHits()
	}
	return snap
}

func write(w model.OutputWriter, columns []string, rows [][]string) error {
	if err := w.Open(columns); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			w.Close()
			return err
		}
	}
	return w.Close()
}
