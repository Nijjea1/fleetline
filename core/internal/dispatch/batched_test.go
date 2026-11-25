package dispatch

import (
	"context"
	"testing"

	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/state"
)

// Rider 1 (Times Square) has two cars: A right next to them, B a bit farther. Rider 2
// (uptown) can only reach A. Greedy serves rider 1 first with A and strands rider 2;
// Batched gives rider 1 car B and rider 2 car A: both served.
func TestBatchedFixesGreedyTrap(t *testing.T) {
	cars := func() *memPool {
		return newMemPool(
			state.Candidate{VehicleID: "A", Kind: model.KindHuman, Lat: 40.7620, Lon: -73.9830},
			state.Candidate{VehicleID: "B", Kind: model.KindHuman, Lat: 40.7500, Lon: -73.9900},
		)
	}
	r1 := timesSq
	r2 := model.RideRequest{TripID: 2, PickLat: 40.7700, PickLon: -73.9790, DropLat: 40.78, DropLon: -73.97}
	pending := []model.RideRequest{r1, r2}
	// Radius 0.8 mi: rider 2 reaches A (~0.6 mi) but not B (~1.4 mi).
	g := &Greedy{Travel: flatModel(), RadiusMi: 0.8, K: 5}
	gs, _ := g.Dispatch(context.Background(), 0, pending, cars())
	if len(gs) != 1 || gs[0].VehicleID != "A" {
		t.Fatalf("greedy: want only rider 1 served with A, got %+v", gs)
	}
	b := &Batched{Travel: flatModel(), RadiusMi: 0.8, K: 5}
	bs, err := b.Dispatch(context.Background(), 0, pending, cars())
	if err != nil || len(bs) != 2 {
		t.Fatalf("batched: want both served, got %+v err %v", bs, err)
	}
	got := map[int64]string{bs[0].TripID: bs[0].VehicleID, bs[1].TripID: bs[1].VehicleID}
	if got[1] != "B" || got[2] != "A" {
		t.Fatalf("batched: want 1->B, 2->A, got %v", got)
	}
}

// With every rider able to reach every car, batching minimizes the *sum* of pickup ETAs.
func TestBatchedMinimizesTotalETA(t *testing.T) {
	pool := newMemPool(
		state.Candidate{VehicleID: "near1", Kind: model.KindHuman, Lat: 40.7582, Lon: -73.9853},
		state.Candidate{VehicleID: "near2", Kind: model.KindHuman, Lat: 40.7700, Lon: -73.9790},
	)
	r1 := timesSq
	r2 := model.RideRequest{TripID: 2, PickLat: 40.7702, PickLon: -73.9788, DropLat: 40.78, DropLon: -73.97}
	b := &Batched{Travel: flatModel(), RadiusMi: 3, K: 5}
	as, _ := b.Dispatch(context.Background(), 0, []model.RideRequest{r1, r2}, pool)
	got := map[int64]string{}
	for _, a := range as {
		got[a.TripID] = a.VehicleID
	}
	if got[1] != "near1" || got[2] != "near2" {
		t.Fatalf("got %v", got)
	}
}

func TestBatchedPickupCapAndAVZone(t *testing.T) {
	// An AV inside the zone may serve an in-zone trip; a car beyond the cap may not.
	pool := newMemPool(
		state.Candidate{VehicleID: "av", Kind: model.KindAV, Lat: 40.7585, Lon: -73.9850},
		state.Candidate{VehicleID: "far", Kind: model.KindHuman, Lat: 40.80, Lon: -73.95},
	)
	b := &Batched{Travel: flatModel(), RadiusMi: 5, K: 5, MaxPickupSec: 300}
	as, _ := b.Dispatch(context.Background(), 0, []model.RideRequest{timesSq}, pool)
	if len(as) != 1 || as[0].VehicleID != "av" {
		t.Fatalf("got %+v", as)
	}
	pool = newMemPool(state.Candidate{VehicleID: "far", Kind: model.KindHuman, Lat: 40.80, Lon: -73.95})
	if as, _ := b.Dispatch(context.Background(), 0, []model.RideRequest{timesSq}, pool); len(as) != 0 {
		t.Fatalf("cap ignored: %+v", as)
	}
}
