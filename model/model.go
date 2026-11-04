// Package model holds the shared records and hook interfaces from docs/spec.md.
package model

// Location is a WGS84 point in signed decimal degrees.
type Location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Settings is the resolved settings object with defaults filled in.
type Settings struct {
	MaxRequestToAssignmentSeconds *int64  `json:"max_request_to_assignment_seconds"`
	MaxRequestToPickupSeconds     *int64  `json:"max_request_to_pickup_seconds"`
	PoolingEnabled                bool    `json:"pooling_enabled"`
	ReassignmentEnabled           bool    `json:"reassignment_enabled"`
	BatchMatchingIntervalSeconds  *int64  `json:"batch_matching_interval_seconds"`
	PickupDwellSeconds            int64   `json:"pickup_dwell_seconds"`
	DropoffDwellSeconds           int64   `json:"dropoff_dwell_seconds"`
	DefaultNumRequestedSeats      int     `json:"default_num_requested_seats"`
	DefaultNumAvailableSeats      int     `json:"default_num_available_seats"`
	AverageSpeedMetersPerSecond   float64 `json:"average_speed_meters_per_second"`

	MatchingHook string `json:"matching_hook"`
	// DriverBehaviorHook names the driver behavior plugin. Empty means the
	// default "simple" plugin; it is left out of the settings JSON so runs with
	// the default keep the settings hash they had before the plugin existed.
	DriverBehaviorHook string `json:"driver_behavior_hook,omitempty"`
	RoutingHook        string `json:"routing_hook"`
	// MaxCandidateCars is the [matching] max_candidate_cars limit: how many
	// cars, closest by straight-line distance to the pickup, the matcher
	// prices per request in each candidate set. Zero means no limit; it is
	// left out of the settings JSON then so existing settings hashes don't
	// change.
	MaxCandidateCars int `json:"max_candidate_cars,omitempty"`
	// Timezone is an optional IANA zone name for showing simulated times as
	// local wall-clock times. It never affects the simulation; it is left out
	// of the settings JSON when empty so existing settings hashes don't change.
	Timezone string `json:"timezone,omitempty"`
	// Router describes the road data behind a non-default router (for
	// example OSRM's version and map extract). It is left nil for the
	// straight-line router so existing settings hashes don't change.
	Router *RouterMetadata `json:"router,omitempty"`
	OSRM   OSRMSettings    `json:"-"`
	IO     IOSettings      `json:"-"`
}

// OSRMSettings holds the [osrm] options of the osrm routing hook.
type OSRMSettings struct {
	URL                 string
	Profile             string
	CachePath           string
	DatasetMetadataPath string
	DatasetName         string
	DatasetDate         string
	DatasetSHA256       string
}

// RouterMetadata identifies the routing engine and road data of a run. It
// is written to run_metadata inside the settings JSON.
type RouterMetadata struct {
	Name          string `json:"name"`
	Profile       string `json:"profile,omitempty"`
	EngineVersion string `json:"engine_version,omitempty"`
	DatasetName   string `json:"dataset_name,omitempty"`
	DatasetDate   string `json:"dataset_date,omitempty"`
	DatasetSHA256 string `json:"dataset_sha256,omitempty"`
}

// IOSettings names the reader and writer plugins and their options.
type IOSettings struct {
	Reader          string
	Writer          string
	RequestsPath    string
	SupplyPath      string
	OutputDirectory string
}

// Request is one ride request input record.
type Request struct {
	RequestID                     string
	RequestedAt                   int64
	Origin                        Location
	Destination                   Location
	NumRequestedSeats             *int
	MaxRequestToAssignmentSeconds *int64
	MaxRequestToPickupSeconds     *int64
}

// Supply is one car input record.
type Supply struct {
	SupplyID          string
	OnlineAt          int64
	OnlineLocation    Location
	OfflineAt         *int64
	NumAvailableSeats *int
}

// Segment is a piece of a leg driven at constant speed.
type Segment struct {
	Polyline        []Location
	DistanceMeters  int64
	DurationSeconds int64
}

// Leg connects two consecutive stops.
type Leg struct {
	Segments        []Segment
	DistanceMeters  int64
	DurationSeconds int64
}

// Router is the routing hook.
type Router interface {
	Route(start Location, waypoints []Location, departAt int64) ([]Leg, error)
}

// LegEstimator is an optional Router extension. Estimate returns the same
// leg durations and distances Route would, but may leave out polylines, so a
// matcher can price candidate plans cheaply. Routers that don't implement it
// are called through Route.
type LegEstimator interface {
	Estimate(start Location, waypoints []Location, departAt int64) ([]Leg, error)
}

// LegPrefetcher is an optional Router extension. Prefetch is a hint that the
// caller is about to ask for legs from each source to each destination, so a
// router can fetch them in one many-to-many request. It must not change any
// result Route or Estimate returns.
type LegPrefetcher interface {
	Prefetch(sources, destinations []Location, departAt int64) error
}

// RouterMetadataProvider is an optional Router extension that describes the
// router for run_metadata.
type RouterMetadataProvider interface {
	RouterMetadata() *RouterMetadata
}

// StopType is pickup or dropoff.
type StopType string

const (
	Pickup  StopType = "pickup"
	Dropoff StopType = "dropoff"
)

// Stop is what the matcher sends: an ordered stop with no route data.
type Stop struct {
	RequestID string   `json:"request_id"`
	Type      StopType `json:"type"`
}

// Waypoint is a stop in a supply's current plan, with its ETA.
type Waypoint struct {
	RequestID         string   `json:"request_id"`
	Type              StopType `json:"type"`
	Location          Location `json:"location"`
	EtaAt             int64    `json:"eta_at"`
	NumRequestedSeats int      `json:"num_requested_seats"`
}

// SupplyView is the matcher's view of one online supply.
type SupplyView struct {
	SupplyID          string
	Location          Location
	State             string
	NumAvailableSeats int
	SeatsInUse        int
	Plan              []Waypoint
}

// RequestView is the matcher's view of one open request.
type RequestView struct {
	RequestID         string
	RequestedAt       int64
	Origin            Location
	Destination       Location
	NumRequestedSeats int
	Status            string
	SupplyID          string
	AssignedAt        *int64
	CancelAt          *int64
}

// MatchInput is everything the matcher sees on one call.
type MatchInput struct {
	Now      int64
	Trigger  string
	Settings Settings
	Supplies []SupplyView  // sorted by supply_id
	Requests []RequestView // sorted by request_id
	Router   Router
}

// PlanChange replaces one supply's plan.
type PlanChange struct {
	SupplyID string
	Plan     []Stop
}

// Matcher is the matching hook.
type Matcher interface {
	Match(input MatchInput) []PlanChange
}

// Triggers.
const (
	TriggerRequest       = "request"
	TriggerSupplyOnline  = "supply_online"
	TriggerSupplyOffline = "supply_offline"
	TriggerBatchTick     = "batch_tick"
)

// Request statuses.
const (
	StatusRequested     = "requested"
	StatusAssigned      = "assigned"
	StatusDriverArrived = "driver_arrived"
	StatusOnTrip        = "on_trip"
	StatusCompleted     = "completed"
	StatusCanceled      = "canceled"
)

// Supply states.
const (
	StateOffline      = "offline"
	StateIdle         = "idle"
	StateEnRoute      = "en_route"
	StateOnTrip       = "on_trip"
	StateGoingOffline = "going_offline"
	// StateRepositioning is an empty-plan car driving to a target its driver
	// behavior chose. It is assignable and counts as idle time and distance.
	StateRepositioning = "repositioning"
)

// DriverState is what a driver behavior sees about one car at one moment.
type DriverState struct {
	Now               int64
	SupplyID          string
	State             string
	Location          Location // current, interpolated position
	OnlineLocation    Location // online_lat and online_lng from the supply input
	OnlineAt          int64
	OfflineAt         *int64
	NumAvailableSeats int
	SeatsInUse        int
	Plan              []Waypoint // current plan with ETAs
}

// DriverBehavior is the driver behavior hook. It decides what a driver does
// on their own, outside the matcher's plans. Implementations must be
// deterministic: the same inputs give the same answers.
type DriverBehavior interface {
	// OnAssignmentOffered is called before a matcher plan change that adds at
	// least one new pickup is applied. Returning false declines the whole
	// change; the car keeps its previous plan and logs plan_declined.
	OnAssignmentOffered(car DriverState, plan []Stop) bool
	// OnIdle is called whenever the car's plan becomes empty while it is
	// online and not going offline (after a dropoff, a cancellation, an
	// unassignment, or arriving at a reposition target). A non-nil result
	// starts a reposition to that location; nil stays put.
	OnIdle(car DriverState) *Location
}

// SettingsReader returns validated settings.
type SettingsReader interface {
	ReadSettings() (Settings, error)
}

// RequestReader yields ride requests.
type RequestReader interface {
	ReadRequests() ([]Request, error)
}

// SupplyReader yields supply records.
type SupplyReader interface {
	ReadSupply() ([]Supply, error)
}

// OutputWriter writes one output stream.
type OutputWriter interface {
	Open(columns []string) error
	Write(values []string) error
	Close() error
}
