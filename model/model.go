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

	MatchingHook string     `json:"matching_hook"`
	RoutingHook  string     `json:"routing_hook"`
	IO           IOSettings `json:"-"`
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
)

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
