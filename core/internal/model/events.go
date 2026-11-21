// Package model defines the messages that flow over Kafka between the world simulator
// and the dispatch system. These structs are the contract; change them carefully.
package model

import "time"

// Kafka topic names.
const (
	TopicPings       = "vehicle.pings"        // key: H3 res-7 cell of the vehicle
	TopicRequests    = "ride.requests"        // key: H3 res-7 cell of the pickup
	TopicAssignments = "dispatch.assignments" // key: vehicle ID
	TopicTripEvents  = "trip.events"          // key: vehicle ID
)

// VehicleKind distinguishes the two halves of the mixed fleet.
type VehicleKind string

const (
	KindHuman VehicleKind = "human"
	KindAV    VehicleKind = "av" // robotaxi
)

// VehicleStatus is the vehicle's state machine:
//
//	idle --assignment--> en_route --arrive--> on_trip --dropoff--> idle
type VehicleStatus string

const (
	StatusIdle    VehicleStatus = "idle"
	StatusEnRoute VehicleStatus = "en_route" // driving to a pickup
	StatusOnTrip  VehicleStatus = "on_trip"  // rider on board
)

// Ping is a GPS update from one vehicle. Sent every few simulated seconds.
type Ping struct {
	VehicleID string        `json:"vehicle_id"`
	Kind      VehicleKind   `json:"kind"`
	Lat       float64       `json:"lat"`
	Lon       float64       `json:"lon"`
	Status    VehicleStatus `json:"status"`
	// Seq is the sequence number of the latest assignment this vehicle has received
	// (0 = none yet). Redis uses it to ignore pings that predate an assignment (D-008).
	Seq int64 `json:"seq"`
	// TS is simulated event time, Unix milliseconds (D-007).
	TS int64 `json:"ts"`
}

// RideRequest is a rider asking for a car, replayed from the real NYC trip.
type RideRequest struct {
	TripID   int64   `json:"trip_id"`
	PickLat  float64 `json:"pick_lat"`
	PickLon  float64 `json:"pick_lon"`
	DropLat  float64 `json:"drop_lat"`
	DropLon  float64 `json:"drop_lon"`
	TripTime int64   `json:"trip_time_s"` // real ride duration once picked up
	TS       int64   `json:"ts"`          // request time, Unix ms (event time)
	// Warmup marks requests replayed only to bring the simulation to a realistic state;
	// they are dispatched normally but excluded from benchmark stats.
	Warmup bool `json:"warmup,omitempty"`
}

// Assignment is the dispatcher's decision: this vehicle picks up this trip.
type Assignment struct {
	TripID    int64       `json:"trip_id"`
	VehicleID string      `json:"vehicle_id"`
	Kind      VehicleKind `json:"kind"`
	Seq       int64       `json:"seq"`    // per-vehicle, strictly increasing
	ETASec    int64       `json:"eta_s"`  // dispatcher's predicted pickup ETA
	TS        int64       `json:"ts"`     // decision time, Unix ms (event time)
	Policy    string      `json:"policy"` // e.g. "greedy", "batch-hungarian"
}

// TripEventType marks trip milestones reported by the world.
type TripEventType string

const (
	TripPickup  TripEventType = "pickup"
	TripDropoff TripEventType = "dropoff"
	TripExpired TripEventType = "expired" // rider gave up before being matched
)

// TripEvent lets the metrics pipeline compute wait times from ground truth.
type TripEvent struct {
	TripID    int64         `json:"trip_id"`
	VehicleID string        `json:"vehicle_id,omitempty"`
	Type      TripEventType `json:"type"`
	TS        int64         `json:"ts"`
}

// Millis converts a time to the Unix-millisecond event-time representation.
func Millis(t time.Time) int64 { return t.UnixMilli() }
