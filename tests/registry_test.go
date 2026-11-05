package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/runner"
)

// An external package registers hooks by name; settings select them.

type countingMatcher struct {
	inner model.Matcher
	calls *int
}

func (m countingMatcher) Match(input model.MatchInput) []model.PlanChange {
	*m.calls++
	return m.inner.Match(input)
}

var externalMatcherCalls int

func init() {
	err := runner.RegisterMatcher("external_counting_matcher", func() (model.Matcher, error) {
		return countingMatcher{inner: matching.NewGreedy(), calls: &externalMatcherCalls}, nil
	})
	if err != nil {
		panic(err)
	}
}

func settingsWithMatcher(t *testing.T, matcher string) string {
	t.Helper()
	base, err := os.ReadFile(filepath.Join("fixtures", "settings", "base_default.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(base), `matching = "reference_test_matcher"`, `matching = "`+matcher+`"`, 1)
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRegisteredExternalMatcherIsSelectedByName(t *testing.T) {
	externalMatcherCalls = 0
	trip := simulateWithSettingsFile(t, settingsWithMatcher(t, "external_counting_matcher"), "one_car_at_kezar_stadium", "single_trip")
	if externalMatcherCalls == 0 {
		t.Fatalf("external matcher was never called")
	}
	if trip["status"] != "completed" {
		t.Fatalf("trip status = %q, want completed", trip["status"])
	}
}

func TestDuplicateMatcherNameIsRejected(t *testing.T) {
	err := runner.RegisterMatcher("greedy_nearest_eta", func() (model.Matcher, error) { return matching.NewGreedy(), nil })
	assertError(t, err, `matching hook "greedy_nearest_eta" is already registered`)
}

func TestDuplicateRouterNameIsRejected(t *testing.T) {
	err := runner.RegisterRouter("osrm", func(model.Settings) (model.Router, error) { return nil, nil })
	assertError(t, err, `routing hook "osrm" is already registered`)
}

func TestDuplicateDriverBehaviorNameIsRejected(t *testing.T) {
	err := runner.RegisterDriverBehavior("simple", func() (model.DriverBehavior, error) { return nil, nil })
	assertError(t, err, `driver behavior plugin "simple" is already registered`)
}

func TestUnknownMatcherNameFromSettingsIsRejected(t *testing.T) {
	_, err := runner.RunFiles(settingsWithMatcher(t, "no_such_matcher"), runner.Paths{
		Supply:          filepath.Join("fixtures", "supply", "one_car_at_kezar_stadium.csv"),
		Requests:        filepath.Join("fixtures", "demand", "single_trip.csv"),
		OutputDirectory: t.TempDir(),
	})
	assertError(t, err, `unknown matching hook "no_such_matcher"`)
}

func TestUnknownRouterNameIsRejected(t *testing.T) {
	_, err := runner.NewRouter("no_such_router", model.Settings{})
	assertError(t, err, `unknown routing hook "no_such_router"`)
}

func assertError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %q", err, want)
	}
}
