// Package tomlsettings is the default SettingsReader: a TOML file.
package tomlsettings

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
)

type file struct {
	Simulation struct {
		MaxRequestToAssignmentSeconds *int64   `toml:"max_request_to_assignment_seconds"`
		MaxRequestToPickupSeconds     *int64   `toml:"max_request_to_pickup_seconds"`
		PoolingEnabled                *bool    `toml:"pooling_enabled"`
		ReassignmentEnabled           *bool    `toml:"reassignment_enabled"`
		BatchMatchingIntervalSeconds  *int64   `toml:"batch_matching_interval_seconds"`
		PickupDwellSeconds            *int64   `toml:"pickup_dwell_seconds"`
		DropoffDwellSeconds           *int64   `toml:"dropoff_dwell_seconds"`
		DefaultNumRequestedSeats      *int     `toml:"default_num_requested_seats"`
		DefaultNumAvailableSeats      *int     `toml:"default_num_available_seats"`
		AverageSpeedMetersPerSecond   *float64 `toml:"average_speed_meters_per_second"`
		Timezone                      string   `toml:"timezone"`
		HandoffsEnabled               *bool    `toml:"handoffs_enabled"`
		MaxHandoffWaitSeconds         *int64   `toml:"max_handoff_wait_seconds"`
	} `toml:"simulation"`
	Hooks struct {
		Matching       string `toml:"matching"`
		DriverBehavior string `toml:"driver_behavior"`
		Routing        string `toml:"routing"`
	} `toml:"hooks"`
	Matching struct {
		MaxCandidateCars *int `toml:"max_candidate_cars"`
	} `toml:"matching"`
	OSRM struct {
		URL                 string `toml:"url"`
		Profile             string `toml:"profile"`
		CachePath           string `toml:"cache_path"`
		DatasetMetadataPath string `toml:"dataset_metadata_path"`
		DatasetName         string `toml:"dataset_name"`
		DatasetDate         string `toml:"dataset_date"`
		DatasetSHA256       string `toml:"dataset_sha256"`
	} `toml:"osrm"`
	IO struct {
		Reader          string `toml:"reader"`
		Writer          string `toml:"writer"`
		RequestsPath    string `toml:"requests_path"`
		SupplyPath      string `toml:"supply_path"`
		OutputDirectory string `toml:"output_directory"`
	} `toml:"io"`
}

// Reader reads settings from a TOML file.
type Reader struct{ Path string }

// ReadSettings decodes the file, rejects unknown keys and fills in defaults.
func (r Reader) ReadSettings() (model.Settings, error) {
	var f file
	meta, err := toml.DecodeFile(r.Path, &f)
	if err != nil {
		return model.Settings{}, err
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return model.Settings{}, fmt.Errorf("%s: unknown settings %v", r.Path, undecoded)
	}
	s := f.Simulation
	rt := f.OSRM
	out := model.Settings{
		MaxRequestToAssignmentSeconds: s.MaxRequestToAssignmentSeconds,
		MaxRequestToPickupSeconds:     s.MaxRequestToPickupSeconds,
		ReassignmentEnabled:           true,
		BatchMatchingIntervalSeconds:  s.BatchMatchingIntervalSeconds,
		DefaultNumRequestedSeats:      4,
		DefaultNumAvailableSeats:      4,
		AverageSpeedMetersPerSecond:   10.0,
		MatchingHook:                  orDefault(f.Hooks.Matching, "greedy_nearest_eta"),
		RoutingHook:                   orDefault(f.Hooks.Routing, "straight_line"),
		OSRM: model.OSRMSettings{
			URL: rt.URL, Profile: rt.Profile, CachePath: rt.CachePath, DatasetMetadataPath: rt.DatasetMetadataPath,
			DatasetName: rt.DatasetName, DatasetDate: rt.DatasetDate, DatasetSHA256: rt.DatasetSHA256,
		},
		IO: model.IOSettings{
			Reader: orDefault(f.IO.Reader, "csv"), Writer: orDefault(f.IO.Writer, "csv"),
			RequestsPath: f.IO.RequestsPath, SupplyPath: f.IO.SupplyPath, OutputDirectory: f.IO.OutputDirectory,
		},
	}
	if s.Timezone != "" {
		if _, err := time.LoadLocation(s.Timezone); err != nil || s.Timezone == "Local" {
			return model.Settings{}, fmt.Errorf("%s: [simulation] timezone %q is not an IANA time zone name", r.Path, s.Timezone)
		}
		out.Timezone = s.Timezone
	}
	if plugin := f.Hooks.DriverBehavior; plugin != "" && plugin != "simple" {
		out.DriverBehaviorHook = plugin // the default stays empty, see model.Settings
	}
	if limit := f.Matching.MaxCandidateCars; limit != nil {
		if *limit < 0 {
			return model.Settings{}, fmt.Errorf("%s: [matching] max_candidate_cars must be 0 (no limit) or more, got %d", r.Path, *limit)
		}
		out.MaxCandidateCars = *limit
	}
	if s.PoolingEnabled != nil {
		out.PoolingEnabled = *s.PoolingEnabled
	}
	if s.ReassignmentEnabled != nil {
		out.ReassignmentEnabled = *s.ReassignmentEnabled
	}
	if s.PickupDwellSeconds != nil {
		out.PickupDwellSeconds = *s.PickupDwellSeconds
	}
	if s.DropoffDwellSeconds != nil {
		out.DropoffDwellSeconds = *s.DropoffDwellSeconds
	}
	if s.DefaultNumRequestedSeats != nil {
		out.DefaultNumRequestedSeats = *s.DefaultNumRequestedSeats
	}
	if s.DefaultNumAvailableSeats != nil {
		out.DefaultNumAvailableSeats = *s.DefaultNumAvailableSeats
	}
	if s.AverageSpeedMetersPerSecond != nil {
		out.AverageSpeedMetersPerSecond = *s.AverageSpeedMetersPerSecond
	}
	if s.HandoffsEnabled != nil {
		out.HandoffsEnabled = *s.HandoffsEnabled
	}
	if s.MaxHandoffWaitSeconds != nil {
		if *s.MaxHandoffWaitSeconds <= 0 {
			return model.Settings{}, fmt.Errorf("%s: max_handoff_wait_seconds must be positive", r.Path)
		}
		out.MaxHandoffWaitSeconds = s.MaxHandoffWaitSeconds
	}
	return out, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
