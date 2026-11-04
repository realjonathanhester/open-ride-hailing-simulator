package viewer

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// MapsSettings holds the Google Maps key and optional Map ID.
type MapsSettings struct {
	GoogleMapsAPIKey string
	GoogleMapsMapID  string
	// DisplayTimezone is [viewer] display_timezone: an IANA zone name for
	// local wall-clock times. Empty means not set.
	DisplayTimezone string
}

// LoadMapsSettings reads [viewer] from a local TOML file (missing is fine),
// then lets GOOGLE_MAPS_API_KEY and GOOGLE_MAPS_MAP_ID override it.
func LoadMapsSettings(path string) (MapsSettings, error) {
	var file struct {
		Viewer struct {
			GoogleMapsAPIKey string `toml:"google_maps_api_key"`
			GoogleMapsMapID  string `toml:"google_maps_map_id"`
			DisplayTimezone  string `toml:"display_timezone"`
		} `toml:"viewer"`
	}
	if path != "" {
		if _, err := toml.DecodeFile(path, &file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return MapsSettings{}, err
		}
	}
	settings := MapsSettings{GoogleMapsAPIKey: file.Viewer.GoogleMapsAPIKey, GoogleMapsMapID: file.Viewer.GoogleMapsMapID,
		DisplayTimezone: file.Viewer.DisplayTimezone}
	if v := os.Getenv("GOOGLE_MAPS_API_KEY"); v != "" {
		settings.GoogleMapsAPIKey = v
	}
	if v := os.Getenv("GOOGLE_MAPS_MAP_ID"); v != "" {
		settings.GoogleMapsMapID = v
	}
	return settings, nil
}

// Timezone sources, in precedence order.
const (
	TimezoneSourceFlag    = "flag"
	TimezoneSourceViewer  = "viewer_settings"
	TimezoneSourceRun     = "run_metadata"
	TimezoneSourceBrowser = "browser"
)

// ResolveTimezone picks the zone for wall-clock times: the -timezone flag,
// then [viewer] display_timezone, then the run's run_metadata timezone. When
// none is set it returns "" and TimezoneSourceBrowser, and the UI uses the
// browser's own zone. Names must be IANA zone names ("Local" is rejected).
func ResolveTimezone(flag, viewerSetting string, run *Run) (name, source string, err error) {
	candidates := []struct{ name, source string }{{flag, TimezoneSourceFlag}, {viewerSetting, TimezoneSourceViewer}}
	if run != nil {
		candidates = append(candidates, struct{ name, source string }{run.Metadata["timezone"], TimezoneSourceRun})
	}
	for _, c := range candidates {
		if c.name == "" {
			continue
		}
		if _, err := time.LoadLocation(c.name); err != nil || c.name == "Local" {
			return "", "", fmt.Errorf("%s timezone %q is not an IANA time zone name", c.source, c.name)
		}
		return c.name, c.source, nil
	}
	return "", TimezoneSourceBrowser, nil
}
