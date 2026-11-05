package runner

import (
	"fmt"
	"sync"

	"github.com/realjonathanhester/ride-hailing-simulator/driverbehavior"
	"github.com/realjonathanhester/ride-hailing-simulator/matching"
	"github.com/realjonathanhester/ride-hailing-simulator/model"
	"github.com/realjonathanhester/ride-hailing-simulator/routing"
)

// MatcherConstructor builds a matching hook.
type MatcherConstructor func() (model.Matcher, error)

// RouterConstructor builds a routing hook from the run settings.
type RouterConstructor func(settings model.Settings) (model.Router, error)

// DriverBehaviorConstructor builds a driver behavior plugin.
type DriverBehaviorConstructor func() (model.DriverBehavior, error)

var (
	registryLock    sync.Mutex
	matchers        = map[string]MatcherConstructor{}
	routers         = map[string]RouterConstructor{}
	driverBehaviors = map[string]DriverBehaviorConstructor{}
)

func register[C any](table map[string]C, kind, name string, constructor C) error {
	registryLock.Lock()
	defer registryLock.Unlock()
	if name == "" {
		return fmt.Errorf("%s name must not be empty", kind)
	}
	if _, taken := table[name]; taken {
		return fmt.Errorf("%s %q is already registered", kind, name)
	}
	table[name] = constructor
	return nil
}

func lookup[C any](table map[string]C, kind, name string) (C, error) {
	registryLock.Lock()
	defer registryLock.Unlock()
	constructor, ok := table[name]
	if !ok {
		return constructor, fmt.Errorf("unknown %s %q", kind, name)
	}
	return constructor, nil
}

// RegisterMatcher makes a matching hook selectable by name in [hooks] matching.
func RegisterMatcher(name string, constructor MatcherConstructor) error {
	return register(matchers, "matching hook", name, constructor)
}

// RegisterRouter makes a routing hook selectable by name in [hooks] routing.
func RegisterRouter(name string, constructor RouterConstructor) error {
	return register(routers, "routing hook", name, constructor)
}

// RegisterDriverBehavior makes a driver behavior plugin selectable by name.
func RegisterDriverBehavior(name string, constructor DriverBehaviorConstructor) error {
	return register(driverBehaviors, "driver behavior plugin", name, constructor)
}

// NewMatcher returns the registered matching hook with this name.
func NewMatcher(name string) (model.Matcher, error) {
	constructor, err := lookup(matchers, "matching hook", name)
	if err != nil {
		return nil, err
	}
	return constructor()
}

// NewRouter returns the registered routing hook with this name.
func NewRouter(name string, settings model.Settings) (model.Router, error) {
	constructor, err := lookup(routers, "routing hook", name)
	if err != nil {
		return nil, err
	}
	return constructor(settings)
}

// NewDriverBehavior returns the registered driver behavior plugin with this
// name; empty means simple.
func NewDriverBehavior(name string) (model.DriverBehavior, error) {
	if name == "" {
		name = driverbehavior.SimpleName
	}
	constructor, err := lookup(driverBehaviors, "driver behavior plugin", name)
	if err != nil {
		return nil, err
	}
	return constructor()
}

func mustRegister(err error) {
	if err != nil {
		panic(err)
	}
}

func init() {
	greedy := func() (model.Matcher, error) { return matching.NewGreedy(), nil }
	mustRegister(RegisterMatcher("greedy_nearest_eta", greedy))
	mustRegister(RegisterMatcher("reference_test_matcher", greedy))
	mustRegister(RegisterRouter("straight_line", func(settings model.Settings) (model.Router, error) {
		return routing.NewStraightLine(settings.AverageSpeedMetersPerSecond)
	}))
	mustRegister(RegisterRouter("osrm", func(settings model.Settings) (model.Router, error) {
		return newOSRM(settings.OSRM)
	}))
	mustRegister(RegisterDriverBehavior(driverbehavior.SimpleName, func() (model.DriverBehavior, error) {
		return driverbehavior.Simple{}, nil
	}))
	mustRegister(RegisterDriverBehavior(driverbehavior.ReturnToOriginName, func() (model.DriverBehavior, error) {
		return driverbehavior.ReturnToOrigin{}, nil
	}))
}
