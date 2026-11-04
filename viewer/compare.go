package viewer

import (
	"encoding/json"
	"sort"
)

// MetricDelta is one row of the comparison table.
type MetricDelta struct {
	Name         string   `json:"name"`
	Group        string   `json:"group"`
	Better       string   `json:"better"`
	Definition   string   `json:"definition_url"`
	A            *float64 `json:"a"`
	B            *float64 `json:"b"`
	Delta        *float64 `json:"delta"`
	PercentDelta *float64 `json:"percent_delta"`
	Verdict      string   `json:"verdict"` // "better", "worse", "same" or "" (neutral or n/a)
}

// SettingDiff is one setting whose value differs between runs.
type SettingDiff struct {
	Name string `json:"name"`
	A    string `json:"a"`
	B    string `json:"b"`
}

// SupplyDelta joins one car's summary across runs.
type SupplyDelta struct {
	SupplyID         string   `json:"supply_id"`
	UtilizationA     *float64 `json:"utilization_a"`
	UtilizationB     *float64 `json:"utilization_b"`
	UtilizationDelta *float64 `json:"utilization_delta"`
	TotalMetersA     *float64 `json:"total_meters_a"`
	TotalMetersB     *float64 `json:"total_meters_b"`
	TotalMetersDelta *float64 `json:"total_meters_delta"`
}

// Comparison is the run comparison page.
type Comparison struct {
	Paired          bool                           `json:"paired"`
	SettingsDiffer  bool                           `json:"settings_differ"`
	VersionMismatch bool                           `json:"version_mismatch"`
	MetadataA       Row                            `json:"metadata_a"`
	MetadataB       Row                            `json:"metadata_b"`
	SettingsDiff    []SettingDiff                  `json:"settings_diff"`
	Metrics         []MetricDelta                  `json:"metrics"`
	Distributions   map[string]map[string][]Bucket `json:"distributions"`
	Fleet           []SupplyDelta                  `json:"fleet"`
}

func subtract(a, b *float64) *float64 {
	if a == nil || b == nil {
		return nil
	}
	return value(*b - *a)
}

// Compare builds the comparison of baseline a against candidate b.
func Compare(a, b *Run) Comparison {
	c := Comparison{
		Paired:          a.Metadata["requests_sha256"] == b.Metadata["requests_sha256"] && a.Metadata["supply_sha256"] == b.Metadata["supply_sha256"],
		SettingsDiffer:  a.Metadata["settings_sha256"] != b.Metadata["settings_sha256"],
		VersionMismatch: a.Metadata["simulator_version"] != b.Metadata["simulator_version"],
		MetadataA:       withoutSettings(a.Metadata), MetadataB: withoutSettings(b.Metadata),
		SettingsDiff: []SettingDiff{}, Distributions: map[string]map[string][]Bucket{}, Fleet: []SupplyDelta{},
	}
	keys := map[string]bool{}
	for k := range a.Settings {
		keys[k] = true
	}
	for k := range b.Settings {
		keys[k] = true
	}
	for k := range keys {
		x, _ := json.Marshal(a.Settings[k])
		y, _ := json.Marshal(b.Settings[k])
		if string(x) != string(y) {
			c.SettingsDiff = append(c.SettingsDiff, SettingDiff{k, string(x), string(y)})
		}
	}
	sort.Slice(c.SettingsDiff, func(i, j int) bool { return c.SettingsDiff[i].Name < c.SettingsDiff[j].Name })

	dashboardA, dashboardB := a.Dashboard(), b.Dashboard()
	metricsB := metricMap(dashboardB.Metrics)
	for _, m := range dashboardA.Metrics {
		other := metricsB[m.Name]
		row := MetricDelta{Name: m.Name, Group: m.Group, Better: m.Better, Definition: m.Definition, A: m.Value, B: other.Value, Delta: subtract(m.Value, other.Value)}
		if row.Delta != nil && *m.Value != 0 {
			row.PercentDelta = value(*row.Delta / *m.Value * 100)
		}
		if row.Delta != nil && m.Better != "" {
			switch {
			case *row.Delta == 0:
				row.Verdict = "same"
			case (*row.Delta > 0) == (m.Better == "higher"):
				row.Verdict = "better"
			default:
				row.Verdict = "worse"
			}
		}
		c.Metrics = append(c.Metrics, row)
	}
	for _, name := range []string{"wait_seconds", "detour_ratio", "utilization"} {
		c.Distributions[name] = map[string][]Bucket{"a": dashboardA.Distributions[name], "b": dashboardB.Distributions[name]}
	}
	ids := map[string]bool{}
	for _, id := range a.supplyIDs {
		ids[id] = true
	}
	for _, id := range b.supplyIDs {
		ids[id] = true
	}
	var sorted []string
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		row := SupplyDelta{SupplyID: id}
		if s, ok := a.supplyByID[id]; ok {
			if u, ok := decimal(s, "utilization"); ok {
				row.UtilizationA = value(u)
			}
			row.TotalMetersA = value(float64(totalMeters(s)))
		}
		if s, ok := b.supplyByID[id]; ok {
			if u, ok := decimal(s, "utilization"); ok {
				row.UtilizationB = value(u)
			}
			row.TotalMetersB = value(float64(totalMeters(s)))
		}
		row.UtilizationDelta = subtract(row.UtilizationA, row.UtilizationB)
		row.TotalMetersDelta = subtract(row.TotalMetersA, row.TotalMetersB)
		c.Fleet = append(c.Fleet, row)
	}
	return c
}

func withoutSettings(metadata Row) Row {
	out := Row{}
	for k, v := range metadata {
		if k != "settings" {
			out[k] = v
		}
	}
	return out
}

// PairedTrip joins one request across both runs.
type PairedTrip struct {
	RequestID               string   `json:"request_id"`
	StatusA                 string   `json:"status_a"`
	StatusB                 string   `json:"status_b"`
	StatusChanged           bool     `json:"status_changed"`
	WaitSecondsDelta        *float64 `json:"wait_seconds_delta"`
	TripDistanceMetersDelta *float64 `json:"trip_distance_meters_delta"`
	DetourRatioDelta        *float64 `json:"detour_ratio_delta"`
	Link                    string   `json:"link"`
}

func cell(row Row, field string, asDecimal bool) *float64 {
	if asDecimal {
		if v, ok := decimal(row, field); ok {
			return value(v)
		}
		return nil
	}
	if v, ok := integer(row, field); ok {
		return value(float64(v))
	}
	return nil
}

// PairedTrips joins Trips on request_id; call only when the runs are paired.
func PairedTrips(a, b *Run) []PairedTrip {
	var out []PairedTrip
	for _, tripA := range a.Trips {
		id := tripA["request_id"]
		tripB, ok := b.tripByID[id]
		if !ok {
			continue
		}
		row := PairedTrip{RequestID: id, StatusA: tripA["status"], StatusB: tripB["status"], StatusChanged: tripA["status"] != tripB["status"],
			WaitSecondsDelta: subtract(cell(tripA, "wait_seconds", false), cell(tripB, "wait_seconds", false)), Link: "/compare/trips/" + id}
		if tripA["status"] == "completed" && tripB["status"] == "completed" {
			row.TripDistanceMetersDelta = subtract(cell(tripA, "trip_distance_meters", false), cell(tripB, "trip_distance_meters", false))
			row.DetourRatioDelta = subtract(cell(tripA, "detour_ratio", true), cell(tripB, "detour_ratio", true))
		}
		out = append(out, row)
	}
	return out
}
