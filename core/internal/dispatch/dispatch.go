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
	NearestIdle(ctx context.Context, kind model.VehicleKind, lat, lon, radiusMi float64, k int) ([]state.Candidate, error)
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
	for _, r := range pending {
		kinds := []model.VehicleKind{model.KindHuman}
		if AVEligible(r) {
			kinds = append(kinds, model.KindAV)
		}
		var opts []option
		for _, k := range kinds {
			cands, err := pool.NearestIdle(ctx, k, r.PickLat, r.PickLon, g.RadiusMi, g.K)
			if err != nil {
				return out, err
			}
			for _, c := range cands {
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
