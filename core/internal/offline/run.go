// Package offline runs the world and a dispatch policy in one deterministic process.
//
// Live mode (worldsim + fleetd) proves the real system works and measures throughput,
// but its results depend on how fast the laptop keeps up. Offline mode removes every
// source of timing noise: no Kafka, no Redis, one goroutine, a simulated clock that
// advances in fixed steps, and the dispatcher sees current vehicle positions. The same
// config and seed always produce byte-identical results, so the only thing that changes
// between two runs is the thing the experiment changes (D-011).
package offline

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/dispatch"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/replay"
	"github.com/Nijjea1/fleetline/core/internal/state"
	"github.com/Nijjea1/fleetline/core/internal/world"
)

// Config is everything that defines one run.
type Config struct {
	Travel   geo.TravelModel
	Policy   dispatch.Policy
	From, To int64         // measured window, event-time ms
	Warmup   time.Duration // simulated before From, excluded from stats
	Drain    time.Duration // simulated after To + Patience so in-flight pickups finish
	Patience time.Duration
	Window   time.Duration // event time between dispatch rounds
	Step     time.Duration // clock resolution
	Supply   Supply
	AVShare  float64
	Seed     uint64
}

// Outcome of one ride request.
type Outcome string

const (
	Served     Outcome = "served"
	Expired    Outcome = "expired"
	Unfinished Outcome = "unfinished" // assigned but not picked up when the run ended
)

// TripResult is one row of per-trip output.
type TripResult struct {
	TripID    int64   `json:"trip_id"`
	RequestTS int64   `json:"request_ts"`
	Warmup    bool    `json:"warmup"`
	Outcome   Outcome `json:"outcome"`
	WaitS     float64 `json:"wait_s"`      // simulated request -> pickup
	RealWaitS int64   `json:"real_wait_s"` // what Uber/Lyft achieved for this request
	Kind      string  `json:"kind,omitempty"`
}

// Sample is the fleet state at one moment, recorded every minute.
type Sample struct {
	TS      int64 `json:"ts"`
	Target  int   `json:"target"`
	Online  int   `json:"online"`
	Busy    int   `json:"busy"`
	Pending int   `json:"pending"`
}

// Result is everything a run produced.
type Result struct {
	Trips   []TripResult
	Samples []Sample
	Rounds  int
	Elapsed time.Duration
}

type runner struct {
	cfg     Config
	w       *world.World
	pool    *state.Mem
	rng     *rand.Rand
	trips   []replay.Trip // sorted by request time
	results map[int64]*TripResult
	pending []model.RideRequest

	online     map[string]bool
	logoffDebt int // vehicles that should log off as soon as they become idle
	nextID     int
}

// Run simulates [From-Warmup, To+Patience+Drain] and returns per-trip outcomes.
func Run(ctx context.Context, cfg Config, trips []replay.Trip) (*Result, error) {
	if cfg.Step <= 0 {
		cfg.Step = time.Second
	}
	if cfg.Window < cfg.Step {
		cfg.Window = cfg.Step
	}
	began := time.Now()
	simStart := cfg.From - cfg.Warmup.Milliseconds()
	stop := cfg.To + cfg.Patience.Milliseconds() + cfg.Drain.Milliseconds()

	r := &runner{
		cfg: cfg, trips: trips, pool: state.NewMem(),
		rng:     rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15)),
		results: map[int64]*TripResult{}, online: map[string]bool{},
	}
	r.w = world.New(world.Config{Travel: cfg.Travel, Patience: cfg.Patience}, simStart, nil)

	// Requests in the simulated span, in order.
	lo := sort.Search(len(trips), func(i int) bool { return trips[i].TS >= simStart })
	hi := sort.Search(len(trips), func(i int) bool { return trips[i].TS >= cfg.To })
	reqs := trips[lo:hi]

	res := &Result{}
	next := 0
	lastRound := int64(-1 << 62)
	step := cfg.Step.Milliseconds()
	for t := simStart; t <= stop; t += step {
		if (t-simStart)%60_000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			r.adjustSupply(t)
			res.Samples = append(res.Samples, r.sample(t))
		}
		for next < len(reqs) && reqs[next].TS <= t {
			q := reqs[next]
			q.Warmup = q.TS < cfg.From
			r.w.Request(q.RideRequest)
			r.pending = append(r.pending, q.RideRequest)
			r.results[q.TripID] = &TripResult{
				TripID: q.TripID, RequestTS: q.TS, Warmup: q.Warmup,
				Outcome: Unfinished, RealWaitS: q.RealWaitS,
			}
			next++
		}
		for _, e := range r.w.Advance(t).Events {
			r.onEvent(e)
		}
		if t-lastRound >= cfg.Window.Milliseconds() {
			lastRound = t
			if err := r.dispatch(ctx, t); err != nil {
				return nil, err
			}
			res.Rounds++
		}
	}

	res.Trips = make([]TripResult, 0, len(r.results))
	for _, tr := range r.results {
		if tr.Outcome == Unfinished && tr.Kind == "" {
			tr.Outcome = Expired // never assigned and the run ended: the rider was not served
		}
		res.Trips = append(res.Trips, *tr)
	}
	sort.Slice(res.Trips, func(i, j int) bool { return res.Trips[i].TripID < res.Trips[j].TripID })
	res.Elapsed = time.Since(began)
	return res, nil
}

func (r *runner) onEvent(e model.TripEvent) {
	switch e.Type {
	case model.TripPickup:
		if tr := r.results[e.TripID]; tr != nil {
			tr.Outcome = Served
			tr.WaitS = float64(e.TS-tr.RequestTS) / 1000
		}
	case model.TripExpired:
		if tr := r.results[e.TripID]; tr != nil {
			tr.Outcome = Expired
		}
	case model.TripDropoff:
		if r.logoffDebt > 0 {
			r.logoffDebt--
			delete(r.online, e.VehicleID) // driver ends their shift after this trip
			return
		}
		v, _ := r.w.Vehicle(e.VehicleID)
		r.pool.AddIdle(v.ID, v.Kind, v.ToLat, v.ToLon)
	}
}

func (r *runner) dispatch(ctx context.Context, now int64) error {
	live := r.pending[:0]
	for _, q := range r.pending {
		if now-q.TS < r.cfg.Patience.Milliseconds() {
			live = append(live, q)
		}
	}
	assigned, err := r.cfg.Policy.Dispatch(ctx, now, live, r.pool)
	if err != nil {
		return err
	}
	done := make(map[int64]bool, len(assigned))
	for _, a := range assigned {
		if err := r.w.Assign(a); err != nil {
			// Offline the dispatcher sees the exact world state, so this is a logic bug.
			return fmt.Errorf("world rejected assignment %+v: %w", a, err)
		}
		done[a.TripID] = true
		r.results[a.TripID].Kind = string(a.Kind)
	}
	left := live[:0]
	for _, q := range live {
		if !done[q.TripID] {
			left = append(left, q)
		}
	}
	r.pending = left
	return nil
}

// adjustSupply logs drivers on or off to track the supply target. Idle drivers log off
// immediately; if there aren't enough, busy ones log off after their current trip.
func (r *runner) adjustSupply(t int64) {
	target := r.cfg.Supply.Target(t)
	have := len(r.online) - r.logoffDebt
	switch {
	case have < target:
		// Cancel pending log-offs first, then spawn new drivers.
		n := target - have
		cancel := min(n, r.logoffDebt)
		r.logoffDebt -= cancel
		for i := 0; i < n-cancel; i++ {
			r.spawn(t)
		}
	case have > target:
		n := have - target
		idle := r.pool.IdleIDs()
		r.rng.Shuffle(len(idle), func(i, j int) { idle[i], idle[j] = idle[j], idle[i] })
		for _, id := range idle {
			if n == 0 {
				break
			}
			r.pool.Remove(id)
			delete(r.online, id)
			n--
		}
		r.logoffDebt += n
	}
}

// spawn adds a driver at the dropoff point of a random real trip requested in the past
// hour: that's where idle cars actually are. Never uses future requests (D-010).
func (r *runner) spawn(t int64) {
	lo := sort.Search(len(r.trips), func(i int) bool { return r.trips[i].TS >= t-3_600_000 })
	hi := sort.Search(len(r.trips), func(i int) bool { return r.trips[i].TS >= t })
	if hi <= lo {
		hi = min(lo+1, len(r.trips)) // start of day: fall back to the earliest trip
	}
	src := r.trips[lo+r.rng.IntN(hi-lo)]
	kind, prefix := model.KindHuman, "h"
	if r.rng.Float64() < r.cfg.AVShare {
		kind, prefix = model.KindAV, "av"
	}
	id := fmt.Sprintf("%s%06d", prefix, r.nextID)
	r.nextID++
	v := &world.Vehicle{ID: id, Kind: kind, ToLat: src.DropLat, ToLon: src.DropLon}
	r.w.AddVehicle(v)
	r.pool.AddIdle(id, kind, v.ToLat, v.ToLon)
	r.online[id] = true
}

func (r *runner) sample(t int64) Sample {
	idle := r.pool.IdleCount(model.KindHuman) + r.pool.IdleCount(model.KindAV)
	return Sample{
		TS: t, Target: r.cfg.Supply.Target(t), Online: len(r.online),
		Busy: len(r.online) - idle, Pending: len(r.pending),
	}
}
