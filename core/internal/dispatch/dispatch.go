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
	var out []model.Assignment

	// Phase 1: fetch candidates for every request in one batch (one Redis round trip).
	var qs []state.Query
	owner := make([]int, 0, len(pending)*2) // query index -> request index
	for i, r := range pending {
		qs = append(qs, state.Query{Kind: model.KindHuman, Lat: r.PickLat, Lon: r.PickLon, RadiusMi: g.RadiusMi, K: g.K})
		owner = append(owner, i)
		if AVEligible(r) {
			qs = append(qs, state.Query{Kind: model.KindAV, Lat: r.PickLat, Lon: r.PickLon, RadiusMi: g.RadiusMi, K: g.K})
			owner = append(owner, i)
		}
	}
	res, err := pool.NearestIdleMany(ctx, qs)
	if err != nil {
		return nil, err
	}
	cands := make([][]state.Candidate, len(pending))
	for qi, cs := range res {
		cands[owner[qi]] = append(cands[owner[qi]], cs...)
	}

	// Phase 2: still greedy, oldest request first. A candidate may have been claimed by an
	// earlier request in this round; Claim fails atomically and we try the next one.
	taken := map[string]bool{}
	for i, r := range pending {
		var opts []option
		for _, c := range cands[i] {
			if !taken[c.VehicleID] {
				opts = append(opts, option{c, g.Travel.TravelSec(c.Lat, c.Lon, r.PickLat, r.PickLon, at)})
			}
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
	}
	return out, nil
}
