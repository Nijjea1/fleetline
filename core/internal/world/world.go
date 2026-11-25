// Package world simulates the city: vehicles driving, riders waiting, trips happening.
//
// It is pure and single-threaded: no Kafka, no clocks, no goroutines. You call
// Advance(t) to move time forward and get back what happened. That makes it deterministic
// (same inputs -> same outputs), which is what the experiments need, and easy to unit test.
// cmd/worldsim wraps it with Kafka for live runs.
package world

import (
	"container/heap"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
)

// Vehicle is one car in the simulated fleet.
type Vehicle struct {
	ID     string
	Kind   model.VehicleKind
	Status model.VehicleStatus
	Seq    int64 // latest assignment sequence received

	// Current leg: drive from (FromLat,FromLon) at LegStart to (ToLat,ToLon) at LegEnd.
	FromLat, FromLon, ToLat, ToLon float64
	LegStart, LegEnd               int64 // ms

	trip     *model.RideRequest // current trip, nil when idle
	nextPing int64
}

// Position returns where the vehicle is at time t (linear along the current leg).
func (v *Vehicle) Position(t int64) (lat, lon float64) {
	if v.Status == model.StatusIdle || v.LegEnd <= v.LegStart {
		return v.ToLat, v.ToLon
	}
	frac := float64(t-v.LegStart) / float64(v.LegEnd-v.LegStart)
	if frac < 0 {
		frac = 0
	}
	return geo.Interpolate(v.FromLat, v.FromLon, v.ToLat, v.ToLon, frac)
}

// Config controls the simulated world.
type Config struct {
	Travel       geo.TravelModel
	PingInterval time.Duration // how often each vehicle reports GPS (Uber uses ~4 s)
	Patience     time.Duration // riders give up if not matched within this
}

// World is the simulated city state.
type World struct {
	cfg      Config
	now      int64
	vehicles []*Vehicle
	byID     map[string]*Vehicle
	waiting  map[int64]*waitingRider // requested, not yet assigned
	expiryQ  []expiry                // sorted by deadline
	legs     legHeap                 // active legs ordered by end time
}

type waitingRider struct {
	req      model.RideRequest
	assigned bool
}

type expiry struct {
	deadline int64
	tripID   int64
}

// New creates a world at time start with the given vehicles placed and idle.
func New(cfg Config, start int64, vehicles []*Vehicle) *World {
	w := &World{cfg: cfg, now: start, byID: map[string]*Vehicle{}, waiting: map[int64]*waitingRider{}}
	interval := cfg.PingInterval.Milliseconds()
	for i, v := range vehicles {
		v.Status = model.StatusIdle
		v.LegStart, v.LegEnd = start, start
		// Stagger pings so the fleet doesn't report in one burst every interval.
		v.nextPing = start + int64(i)%max(interval, 1)
		w.vehicles = append(w.vehicles, v)
		w.byID[v.ID] = v
	}
	return w
}

// Now is the world's current simulated time (ms).
func (w *World) Now() int64 { return w.now }

// Vehicles exposes the fleet (read-only use).
func (w *World) Vehicles() []*Vehicle { return w.vehicles }

// Request registers a rider asking for a car at r.TS.
func (w *World) Request(r model.RideRequest) {
	w.waiting[r.TripID] = &waitingRider{req: r}
	w.expiryQ = append(w.expiryQ, expiry{deadline: r.TS + w.cfg.Patience.Milliseconds(), tripID: r.TripID})
}

// Assign applies a dispatcher decision at the current time. It returns an error for
// assignments the world can't honor; the vehicle still records the seq so its next ping
// acknowledges it and the dispatcher's state store releases it (D-008).
// Waiting reports whether a rider is still waiting for a car (not expired, not assigned).
func (w *World) Waiting(tripID int64) bool {
	wr, ok := w.waiting[tripID]
	return ok && !wr.assigned
}

func (w *World) Assign(a model.Assignment) error {
	v, ok := w.byID[a.VehicleID]
	if !ok {
		return fmt.Errorf("unknown vehicle %s", a.VehicleID)
	}
	if a.Seq > v.Seq {
		v.Seq = a.Seq
	}
	wr, ok := w.waiting[a.TripID]
	switch {
	case !ok:
		return fmt.Errorf("trip %d no longer waiting (expired or unknown)", a.TripID)
	case wr.assigned:
		return fmt.Errorf("trip %d already assigned", a.TripID)
	case v.Status != model.StatusIdle:
		return fmt.Errorf("vehicle %s is %s, not idle", v.ID, v.Status)
	}
	wr.assigned = true
	req := wr.req
	v.trip = &req
	lat, lon := v.Position(w.now)
	w.startLeg(v, model.StatusEnRoute, lat, lon, req.PickLat, req.PickLon,
		int64(w.cfg.Travel.TravelSec(lat, lon, req.PickLat, req.PickLon, w.clock())*1000))
	return nil
}

func (w *World) clock() time.Time { return time.UnixMilli(w.now).UTC() }

func (w *World) startLeg(v *Vehicle, s model.VehicleStatus, fLat, fLon, tLat, tLon float64, durMs int64) {
	v.Status = s
	v.FromLat, v.FromLon, v.ToLat, v.ToLon = fLat, fLon, tLat, tLon
	v.LegStart, v.LegEnd = w.now, w.now+durMs
	heap.Push(&w.legs, legEnd{at: v.LegEnd, v: v})
}

// Step is everything that happened during one Advance.
type Step struct {
	Events []model.TripEvent
	Pings  []model.Ping
}

// Advance moves the world to time t, completing legs that end on the way and emitting
// pings that come due. Leg completions are processed in time order.
func (w *World) Advance(t int64) Step {
	var out Step
	if t < w.now {
		return out
	}
	// Pop leg completions in global time order. A pickup immediately schedules the trip
	// leg (another heap entry), so chained legs that both end before t are handled in
	// the right order relative to every other vehicle. Cost is O(log n) per completion
	// instead of scanning the whole fleet every step.
	for w.legs.Len() > 0 && w.legs[0].at <= t {
		e := heap.Pop(&w.legs).(legEnd)
		if e.v.Status == model.StatusIdle || e.v.LegEnd != e.at {
			continue // stale entry
		}
		w.now = e.at
		out.Events = append(out.Events, w.finishLeg(e.v))
	}
	w.now = t
	out.Events = append(out.Events, w.expire()...)

	if w.cfg.PingInterval <= 0 {
		return out // offline mode: nobody listens to pings
	}
	for _, v := range w.vehicles {
		if v.nextPing > t {
			continue
		}
		lat, lon := v.Position(t)
		out.Pings = append(out.Pings, model.Ping{
			VehicleID: v.ID, Kind: v.Kind, Lat: lat, Lon: lon, Status: v.Status, Seq: v.Seq, TS: t,
		})
		for v.nextPing <= t {
			v.nextPing += w.cfg.PingInterval.Milliseconds()
		}
	}
	return out
}

// finishLeg handles one arrival: at the pickup (start the trip) or at the dropoff
// (become idle).
func (w *World) finishLeg(v *Vehicle) model.TripEvent {
	tripID := v.trip.TripID
	if v.Status == model.StatusEnRoute {
		delete(w.waiting, tripID)
		// Ride duration is the real one from the data, so dispatch only changes the wait.
		w.startLeg(v, model.StatusOnTrip, v.trip.PickLat, v.trip.PickLon,
			v.trip.DropLat, v.trip.DropLon, v.trip.TripTime*1000)
		return model.TripEvent{TripID: tripID, VehicleID: v.ID, Type: model.TripPickup, TS: w.now}
	}
	v.Status = model.StatusIdle
	v.FromLat, v.FromLon = v.ToLat, v.ToLon
	v.trip = nil
	return model.TripEvent{TripID: tripID, VehicleID: v.ID, Type: model.TripDropoff, TS: w.now}
}

// AddVehicle puts a new idle vehicle into the world at the current time (a driver
// logging on). The caller decides whether it is offered to the dispatcher.
func (w *World) AddVehicle(v *Vehicle) {
	v.Status = model.StatusIdle
	v.LegStart, v.LegEnd = w.now, w.now
	v.nextPing = w.now
	w.vehicles = append(w.vehicles, v)
	w.byID[v.ID] = v
}

// Vehicle looks up a vehicle by ID.
func (w *World) Vehicle(id string) (*Vehicle, bool) {
	v, ok := w.byID[id]
	return v, ok
}

// legEnd is a heap entry: vehicle v's current leg ends at `at`.
type legEnd struct {
	at int64
	v  *Vehicle
}

// legHeap is a min-heap by end time, tie-broken by vehicle ID for determinism.
type legHeap []legEnd

func (h legHeap) Len() int { return len(h) }
func (h legHeap) Less(i, j int) bool {
	if h[i].at != h[j].at {
		return h[i].at < h[j].at
	}
	return h[i].v.ID < h[j].v.ID
}
func (h legHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *legHeap) Push(x any)   { *h = append(*h, x.(legEnd)) }
func (h *legHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// expire removes riders whose patience ran out before anyone was assigned.
func (w *World) expire() []model.TripEvent {
	var ev []model.TripEvent
	// Requests arrive in time order and patience is constant, so expiryQ is already
	// sorted by deadline: pop from the front, no sorting needed.
	i := 0
	for ; i < len(w.expiryQ) && w.expiryQ[i].deadline <= w.now; i++ {
		e := w.expiryQ[i]
		if wr, ok := w.waiting[e.tripID]; ok && !wr.assigned {
			delete(w.waiting, e.tripID)
			ev = append(ev, model.TripEvent{TripID: e.tripID, Type: model.TripExpired, TS: e.deadline})
		}
	}
	w.expiryQ = w.expiryQ[i:]
	return ev
}

// Point is a lat/lon location.
type Point struct{ Lat, Lon float64 }

// SpawnFleet places n idle vehicles at locations drawn (seeded, with replacement) from
// `at`. avShare of them are robotaxis.
func SpawnFleet(n int, avShare float64, at []Point, seed uint64) []*Vehicle {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	out := make([]*Vehicle, n)
	nAV := int(float64(n) * avShare)
	for i := range out {
		r := at[rng.IntN(len(at))]
		kind, prefix := model.KindHuman, "h"
		if i < nAV {
			kind, prefix = model.KindAV, "av"
		}
		out[i] = &Vehicle{ID: fmt.Sprintf("%s%05d", prefix, i), Kind: kind, ToLat: r.Lat, ToLon: r.Lon}
	}
	return out
}
