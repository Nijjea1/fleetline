// Package dispatch decides which vehicle serves which rider.
//
// A Policy sees the pending requests and a Pool of idle vehicles and returns assignments.
// Phase 1 has Greedy; Phase 2 adds batched policies (Hungarian, OR-Tools, CUDA) behind the
// same interface, so experiments change exactly one thing: the policy.
package dispatch

import (
	"context"
	"sort"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/state"
)

// Pool is the view of vehicle supply a policy needs. *state.Store implements it.
type Pool interface {
	NearestIdleMany(ctx context.Context, qs []state.Query) ([][]state.Candidate, error)
	Claim(ctx context.Context, vehicleID string, kind model.VehicleKind) (int64, bool, error)
}

// Policy turns pending requests into assignments at simulated time now (ms).
type Policy interface {
	Name() string
	Dispatch(ctx context.Context, now int64, pending []model.RideRequest, pool Pool) ([]model.Assignment, error)
}

// AVEligible reports whether a robotaxi may serve this trip: both ends inside the zone.
func AVEligible(r model.RideRequest) bool {
	return geo.RobotaxiZone.Contains(r.PickLat, r.PickLon) && geo.RobotaxiZone.Contains(r.DropLat, r.DropLon)
}

// Greedy assigns each request, oldest first, to the idle vehicle with the lowest pickup
// ETA the moment it is seen. It is the classic baseline: fast and simple, but "myopic":
// it can grab the one car that a later rider needed far more. E2 measures that cost.
type Greedy struct {
	Travel   geo.TravelModel
	RadiusMi float64 // only consider vehicles this close (straight line)
	K        int     // candidates per vehicle kind
	// MaxPickupSec rejects matches whose pickup ETA exceeds this (0 = no cap). Without a
	// cap, a shortage makes greedy send far-away cars; long pickup drives keep cars busy,
	// which deepens the shortage: the "wild goose chase". Real platforms cap pickup ETAs.
	MaxPickupSec float64
	// NoSkip disables the skip-unchanged optimization (for A/B checks that it changes
	// nothing but speed).
	NoSkip bool

	// Requests that found no vehicle, indexed by grid cell. A failed request can only
	// succeed once a vehicle becomes idle near it, so each round we let the *new idle
	// vehicles* mark nearby failed requests for a retry, instead of every failed
	// request re-searching. Only used when the pool supports change tracking.
	failed     map[int64]failedAttempt
	failedCell map[[2]int32][]int64
	lastGen    int64
}

type failedAttempt struct {
	hour int // speeds change by hour, which can bring a car under the cap
}

// ChangeTracker is an optional Pool capability (state.Mem has it; Redis doesn't).
type ChangeTracker interface {
	Gen() int64
	AdditionsSince(gen int64) []state.Addition
}

// skipUnchanged drops requests that failed before and have no new idle vehicle nearby.
func (g *Greedy) skipUnchanged(tr ChangeTracker, pending []model.RideRequest, radius float64, hour int) []model.RideRequest {
	if g.failed == nil {
		g.failed, g.failedCell = map[int64]failedAttempt{}, map[[2]int32][]int64{}
	}
	// Mark failed requests within reach of each newly idle vehicle.
	dirty := map[int64]bool{}
	reach := int32(radius/state.CellMi) + 1
	for _, a := range tr.AdditionsSince(g.lastGen) {
		la, lo := state.Cell(a.Lat, a.Lon)
		for dl := -reach; dl <= reach; dl++ {
			for dn := -reach; dn <= reach; dn++ {
				for _, id := range g.failedCell[[2]int32{la + dl, lo + dn}] {
					dirty[id] = true
				}
			}
		}
	}
	g.lastGen = tr.Gen()

	prev := g.failed
	g.failed, g.failedCell = make(map[int64]failedAttempt, len(prev)), map[[2]int32][]int64{}
	kept := pending[:0:0]
	for _, r := range pending {
		if f, ok := prev[r.TripID]; ok && f.hour == hour && !dirty[r.TripID] {
			g.remember(r, hour) // still hopeless; carry it forward
			continue
		}
		kept = append(kept, r)
	}
	return kept
}

func (g *Greedy) remember(r model.RideRequest, hour int) {
	g.failed[r.TripID] = failedAttempt{hour: hour}
	la, lo := state.Cell(r.PickLat, r.PickLon)
	g.failedCell[[2]int32{la, lo}] = append(g.failedCell[[2]int32{la, lo}], r.TripID)
}

// searchRadius shrinks the radius to what the pickup cap allows at this hour: a car
// farther than cap x speed / detour (straight line) can never be under the cap.
func (g *Greedy) searchRadius(at time.Time) float64 {
	if g.MaxPickupSec <= 0 {
		return g.RadiusMi
	}
	reach := g.MaxPickupSec * g.Travel.SpeedMPS(at) / g.Travel.DetourFactor
	return min(g.RadiusMi, reach)
}

// Name implements Policy.
func (g *Greedy) Name() string { return "greedy" }

type option struct {
	c   state.Candidate
	eta float64
}

// Dispatch implements Policy.
func (g *Greedy) Dispatch(ctx context.Context, now int64, pending []model.RideRequest, pool Pool) ([]model.Assignment, error) {
	at := time.UnixMilli(now).UTC()
	radius := g.searchRadius(at)
	var out []model.Assignment

	// Skip requests that failed before and whose neighborhood hasn't gained a vehicle
	// since. This doesn't change any decision, only avoids repeating a search that
	// would return nothing new.
	tracker, tracking := pool.(ChangeTracker)
	tracking = tracking && !g.NoSkip
	if tracking {
		pending = g.skipUnchanged(tracker, pending, radius, at.Hour())
	}

	// Phase 1: fetch candidates for every request in one batch (one Redis round trip).
	var qs []state.Query
	owner := make([]int, 0, len(pending)*2) // query index -> request index
	for i, r := range pending {
		qs = append(qs, state.Query{Kind: model.KindHuman, Lat: r.PickLat, Lon: r.PickLon, RadiusMi: radius, K: g.K})
		owner = append(owner, i)
		if AVEligible(r) {
			qs = append(qs, state.Query{Kind: model.KindAV, Lat: r.PickLat, Lon: r.PickLon, RadiusMi: radius, K: g.K})
			owner = append(owner, i)
		}
	}
	res, err := pool.NearestIdleMany(ctx, qs)
	if err != nil {
		return nil, err
	}
	cands := make([][]state.Candidate, len(pending))
	truncated := make([]bool, len(pending))
	for qi, cs := range res {
		cands[owner[qi]] = append(cands[owner[qi]], cs...)
		if len(cs) >= g.K {
			truncated[owner[qi]] = true // more idle cars may exist beyond the top K
		}
	}

	// Phase 2: still greedy, oldest request first. A candidate may have been claimed by an
	// earlier request in this round; Claim fails atomically and we try the next one.
	taken := map[string]bool{}
	for i, r := range pending {
		var opts []option
		for _, c := range cands[i] {
			if taken[c.VehicleID] {
				continue
			}
			eta := g.Travel.TravelSec(c.Lat, c.Lon, r.PickLat, r.PickLon, at)
			if g.MaxPickupSec > 0 && eta > g.MaxPickupSec {
				continue
			}
			opts = append(opts, option{c, eta})
		}
		// Stable tie-break on ID keeps runs reproducible.
		sort.Slice(opts, func(i, j int) bool {
			if opts[i].eta != opts[j].eta {
				return opts[i].eta < opts[j].eta
			}
			return opts[i].c.VehicleID < opts[j].c.VehicleID
		})
		for _, o := range opts {
			seq, ok, err := pool.Claim(ctx, o.c.VehicleID, o.c.Kind)
			if err != nil {
				return out, err
			}
			taken[o.c.VehicleID] = true
			if !ok {
				continue // someone else took it; try the next best
			}
			out = append(out, model.Assignment{
				TripID: r.TripID, VehicleID: o.c.VehicleID, Kind: o.c.Kind, Seq: seq,
				ETASec: int64(o.eta), TS: now, Policy: g.Name(),
			})
			break
		}
		// Only a search that saw *every* idle car in range proves the request hopeless
		// until a new car appears. If the top-K list was full, a car beyond it may still
		// be free next round, so don't memoize.
		failed := len(out) == 0 || out[len(out)-1].TripID != r.TripID
		if tracking && failed && !truncated[i] {
			g.remember(r, at.Hour())
		}
	}
	return out, nil
}
