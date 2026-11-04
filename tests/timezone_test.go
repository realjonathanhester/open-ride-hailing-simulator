package tests

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/plugins/tomlsettings"
	"github.com/realjonathanhester/ride-hailing-simulator/viewer"
)

// Local wall-clock time: the optional [simulation] timezone, its run_metadata
// column and the viewer's timezone resolution and API (docs/tooling_spec.md).

func TestRunMetadataOmitsTimezoneWhenUnset(t *testing.T) {
	o := simulate(t, "base_default", "one_car_at_kezar_stadium", "single_trip")
	header := strings.SplitN(o.files["run_metadata.csv"], "\n", 2)[0]
	if strings.Contains(header, "timezone") {
		t.Fatalf("run_metadata header has a timezone column without the setting: %s", header)
	}
}

func TestRunMetadataRecordsTimezone(t *testing.T) {
	base := simulate(t, "base_default", "one_car_at_kezar_stadium", "single_trip")
	zoned := simulate(t, "with_timezone", "one_car_at_kezar_stadium", "single_trip")
	metadata := zoned.runMetadata()
	assertEquals(t, metadata["timezone"], "America/Los_Angeles")
	if !strings.Contains(metadata["settings"], `"timezone":"America/Los_Angeles"`) {
		t.Fatalf("settings JSON lacks timezone: %s", metadata["settings"])
	}
	// The zone is display-only: every other output is unchanged.
	for _, name := range []string{"trips.csv", "supply_event_log.csv", "supply_summary.csv"} {
		assertFile(t, name, zoned.files[name], base.files[name])
	}
}

func TestSettingsRejectUnknownTimezone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte("[simulation]\ntimezone = \"Mars/Olympus_Mons\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (tomlsettings.Reader{Path: path}).ReadSettings(); err == nil {
		t.Fatal("expected an error for an unknown timezone")
	}
}

func TestViewerTimezonePrecedence(t *testing.T) {
	zoned := simulateForViewer(t, "with_timezone", "one_car_at_kezar_stadium", "single_trip")
	plain := simulateForViewer(t, "base_default", "one_car_at_kezar_stadium", "single_trip")
	cases := []struct {
		flag, viewerSetting string
		run                 *viewer.Run
		name, source        string
	}{
		{"Europe/Paris", "America/Chicago", zoned, "Europe/Paris", viewer.TimezoneSourceFlag},
		{"", "America/Chicago", zoned, "America/Chicago", viewer.TimezoneSourceViewer},
		{"", "", zoned, "America/Los_Angeles", viewer.TimezoneSourceRun},
		{"", "", plain, "", viewer.TimezoneSourceBrowser},
	}
	for _, c := range cases {
		name, source, err := viewer.ResolveTimezone(c.flag, c.viewerSetting, c.run)
		if err != nil {
			t.Fatal(err)
		}
		assertEquals(t, name+"|"+source, c.name+"|"+c.source)
	}
	if _, _, err := viewer.ResolveTimezone("Not/AZone", "", plain); err == nil {
		t.Fatal("expected an error for an unknown -timezone")
	}
	if _, _, err := viewer.ResolveTimezone("", "Local", plain); err == nil {
		t.Fatal("expected an error for display_timezone \"Local\"")
	}
}

func TestViewerConfigExposesTimezone(t *testing.T) {
	run := simulateForViewer(t, "with_timezone", "one_car_at_kezar_stadium", "single_trip")
	name, source, err := viewer.ResolveTimezone("", "", run)
	if err != nil {
		t.Fatal(err)
	}
	server := serve(t, viewer.Config{Run: run, Timezone: name, TimezoneSource: source})
	var config map[string]any
	assertEquals(t, strconv.Itoa(get(t, server.URL+"/api/config", &config)), "200")
	assertEquals(t, config["timezone"].(string), "America/Los_Angeles")
	assertEquals(t, config["timezone_source"].(string), "run_metadata")

	browser := serve(t, viewer.Config{Run: simulateForViewer(t, "base_default", "one_car_at_kezar_stadium", "single_trip")})
	assertEquals(t, strconv.Itoa(get(t, browser.URL+"/api/config", &config)), "200")
	assertEquals(t, config["timezone"].(string), "")
	assertEquals(t, config["timezone_source"].(string), "browser")
}
