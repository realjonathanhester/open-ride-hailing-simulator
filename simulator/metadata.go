package simulator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/realjonathanhester/ride-hailing-simulator/model"
)

// Version is the simulator version written to run_metadata.
const Version = "0.2.0"

// RunMetadataColumns is the run_metadata column order from docs/spec.md.
var RunMetadataColumns = []string{"simulator_version", "requests_sha256", "supply_sha256", "settings_sha256", "settings",
	"simulation_start_at", "simulation_end_at"}

type canonicalRequest struct {
	RequestID                     string         `json:"request_id"`
	RequestedAt                   int64          `json:"requested_at"`
	Origin                        model.Location `json:"origin"`
	Destination                   model.Location `json:"destination"`
	NumRequestedSeats             int            `json:"num_requested_seats"`
	MaxRequestToAssignmentSeconds *int64         `json:"max_request_to_assignment_seconds"`
	MaxRequestToPickupSeconds     *int64         `json:"max_request_to_pickup_seconds"`
}

type canonicalSupply struct {
	SupplyID          string         `json:"supply_id"`
	OnlineAt          int64          `json:"online_at"`
	OnlineLocation    model.Location `json:"online_location"`
	OfflineAt         *int64         `json:"offline_at"`
	NumAvailableSeats int            `json:"num_available_seats"`
}

func sha256JSON(v any) (string, []byte) {
	encoded, _ := json.Marshal(v)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), encoded
}

// runMetadata hashes the parsed records (sorted by ID, defaults filled in),
// so reformatting an input file doesn't change the hash.
func (s *Simulator) runMetadata(requests []model.Request, supplies []model.Supply) [][]string {
	var canonicalRequests []canonicalRequest
	for _, id := range s.requestOrder {
		r := s.requests[id]
		canonicalRequests = append(canonicalRequests, canonicalRequest{id, r.input.RequestedAt, r.input.Origin, r.input.Destination, r.seats,
			r.input.MaxRequestToAssignmentSeconds, r.input.MaxRequestToPickupSeconds})
	}
	var canonicalSupplies []canonicalSupply
	for _, id := range s.carOrder {
		c := s.cars[id]
		canonicalSupplies = append(canonicalSupplies, canonicalSupply{id, c.input.OnlineAt, c.input.OnlineLocation, c.input.OfflineAt, c.capacity})
	}
	sort.Slice(canonicalSupplies, func(i, j int) bool { return canonicalSupplies[i].SupplyID < canonicalSupplies[j].SupplyID })
	requestsHash, _ := sha256JSON(canonicalRequests)
	supplyHash, _ := sha256JSON(canonicalSupplies)
	settingsHash, settingsJSON := sha256JSON(s.settings)
	return [][]string{{Version, requestsHash, supplyHash, settingsHash, string(settingsJSON), formatInt(s.startAt), formatInt(s.endAt)}}
}
