package dispatch

import (
	"context"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/assign"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/state"
)

// Batched collects every waiting request and solves them together: the assignment of
// riders to cars with the lowest total pickup ETA (after matching as many riders as
// possible). Greedy decides one rider at a time and can take the one car a later rider
// needed; Batched sees the whole round at once.
//
// It shares Greedy's candidate search, search radius and pickup cap, so an A/B between
// the two changes only the matching step. How often it runs is the simulator's dispatch
// window (E3: longer windows mean bigger batches but riders wait for the round).
type Batched struct {
	Travel       geo.TravelModel
	RadiusMi     float64
	K            int
	MaxPickupSec float64
}

// unmatchedCost must exceed any possible change in total ETA, so the solver never leaves
// a rider unmatched to save pickup time elsewhere.
const unmatchedCost = 1e9

// Name implements Policy.
func (b *Batched) Name() string { return "batched" }

// Dispatch implements Policy.
func (b *Batched) Dispatch(ctx context.Context, now int64, pending []model.RideRequest, pool Pool) ([]model.Assignment, error) {
	at := time.UnixMilli(now).UTC()
	radius := searchRadius(b.Travel, b.RadiusMi, b.MaxPickupSec, at)
	cands, _, err := fetchCandidates(ctx, pending, pool, radius, b.K)
	if err != nil {
		return nil, err
	}

	// Columns = distinct candidate cars, in first-seen order (deterministic).
	colOf := map[string]int{}
	var cars []state.Candidate
	var edges []assign.Edge
	for i, r := range pending {
		for _, c := range cands[i] {
			eta := b.Travel.TravelSec(c.Lat, c.Lon, r.PickLat, r.PickLon, at)
			if b.MaxPickupSec > 0 && eta > b.MaxPickupSec {
				continue
			}
			j, ok := colOf[c.VehicleID]
			if !ok {
				j = len(cars)
				colOf[c.VehicleID] = j
				cars = append(cars, c)
			}
			edges = append(edges, assign.Edge{Row: i, Col: j, Cost: eta})
		}
	}
	if len(edges) == 0 {
		return nil, nil
	}

	var out []model.Assignment
	for i, j := range assign.Solve(len(pending), len(cars), edges, unmatchedCost) {
		if j < 0 {
			continue
		}
		r, c := pending[i], cars[j]
		seq, ok, err := pool.Claim(ctx, c.VehicleID, c.Kind)
		if err != nil {
			return out, err
		}
		if !ok {
			continue // taken since the search (live mode, another dispatcher); retry next round
		}
		out = append(out, model.Assignment{
			TripID: r.TripID, VehicleID: c.VehicleID, Kind: c.Kind, Seq: seq,
			ETASec: int64(b.Travel.TravelSec(c.Lat, c.Lon, r.PickLat, r.PickLon, at)), TS: now, Policy: b.Name(),
		})
	}
	return out, nil
}
