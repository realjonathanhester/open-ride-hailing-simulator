package viewer

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

// MapsSettings holds the Google Maps key and optional Map ID.
type MapsSettings struct {
	GoogleMapsAPIKey string
	GoogleMapsMapID  string
}

// LoadMapsSettings reads [viewer] from a local TOML file (missing is fine),
// then lets GOOGLE_MAPS_API_KEY and GOOGLE_MAPS_MAP_ID override it.
func LoadMapsSettings(path string) (MapsSettings, error) {
	var file struct {
		Viewer struct {
			GoogleMapsAPIKey string `toml:"google_maps_api_key"`
			GoogleMapsMapID  string `toml:"google_maps_map_id"`
		} `toml:"viewer"`
	}
	if path != "" {
		if _, err := toml.DecodeFile(path, &file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return MapsSettings{}, err
		}
	}
	settings := MapsSettings{GoogleMapsAPIKey: file.Viewer.GoogleMapsAPIKey, GoogleMapsMapID: file.Viewer.GoogleMapsMapID}
	if v := os.Getenv("GOOGLE_MAPS_API_KEY"); v != "" {
		settings.GoogleMapsAPIKey = v
	}
	if v := os.Getenv("GOOGLE_MAPS_MAP_ID"); v != "" {
		settings.GoogleMapsMapID = v
	}
	return settings, nil
}
