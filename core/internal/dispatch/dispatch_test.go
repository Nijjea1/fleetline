package dispatch

import (
	"context"
	"sort"
	"testing"

	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/state"
)

// memPool is an in-memory Pool for tests: brute-force nearest search.
type memPool struct {
	idle map[string]state.Candidate
	seq  map[string]int64
}

func newMemPool(cs ...state.Candidate) *memPool {
	p := &memPool{idle: map[string]state.Candidate{}, seq: map[string]int64{}}
	for _, c := range cs {
		p.idle[c.VehicleID] = c
	}
	return p
}

func (p *memPool) NearestIdle(_ context.Context, kind model.VehicleKind, lat, lon, r float64, k int) ([]state.Candidate, error) {
	var out []state.Candidate
	for _, c := range p.idle {
		if c.Kind != kind {
			continue
		}
		c.DistMi = geo.HaversineMi(lat, lon, c.Lat, c.Lon)
		if c.DistMi <= r {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DistMi < out[j].DistMi })
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

func (p *memPool) Claim(_ context.Context, id string, _ model.VehicleKind) (int64, bool, error) {
	if _, ok := p.idle[id]; !ok {
		return 0, false, nil
	}
	delete(p.idle, id)
	p.seq[id]++
	return p.seq[id], true, nil
}

func flatModel() geo.TravelModel {
	var m geo.TravelModel
	m.DetourFactor = 1.3
	for h := range m.SpeedMPHByHour {
		m.SpeedMPHByHour[h] = 10
	}
	return m
}

var timesSq = model.RideRequest{TripID: 1, PickLat: 40.7580, PickLon: -73.9855, DropLat: 40.7359, DropLon: -73.9911}

func TestGreedyPicksNearest(t *testing.T) {
	pool := newMemPool(
		state.Candidate{VehicleID: "far", Kind: model.KindHuman, Lat: 40.7700, Lon: -73.9800},
		state.Candidate{VehicleID: "near", Kind: model.KindHuman, Lat: 40.7585, Lon: -73.9850},
	)
	g := &Greedy{Travel: flatModel(), RadiusMi: 3, K: 5}
	as, err := g.Dispatch(context.Background(), 0, []model.RideRequest{timesSq}, pool)
	if err != nil || len(as) != 1 || as[0].VehicleID != "near" || as[0].Seq != 1 {
		t.Fatalf("got %+v err %v", as, err)
	}
}

func TestGreedyIsMyopic(t *testing.T) {
	// One car between two riders. Greedy gives it to whoever asked first, even though
	// rider 2 has no other option nearby. This is the weakness batching fixes.
	car := state.Candidate{VehicleID: "only", Kind: model.KindHuman, Lat: 40.7580, Lon: -73.9855}
	pool := newMemPool(car)
	r1 := timesSq
	r2 := timesSq
	r2.TripID = 2
	g := &Greedy{Travel: flatModel(), RadiusMi: 3, K: 5}
	as, _ := g.Dispatch(context.Background(), 0, []model.RideRequest{r1, r2}, pool)
	if len(as) != 1 || as[0].TripID != 1 {
		t.Fatalf("first-come-first-served expected, got %+v", as)
	}
}

func TestRobotaxiOnlyInsideZone(t *testing.T) {
	av := state.Candidate{VehicleID: "av1", Kind: model.KindAV, Lat: 40.7581, Lon: -73.9856}
	g := &Greedy{Travel: flatModel(), RadiusMi: 3, K: 5}

	// Inside the zone: the AV is closest and wins.
	as, _ := g.Dispatch(context.Background(), 0, []model.RideRequest{timesSq}, newMemPool(av))
	if len(as) != 1 || as[0].VehicleID != "av1" {
		t.Fatalf("AV should serve in-zone trip, got %+v", as)
	}
	// Dropoff in Brooklyn: AV not eligible, so nobody is assigned.
	out := timesSq
	out.DropLat, out.DropLon = 40.6782, -73.9442
	as, _ = g.Dispatch(context.Background(), 0, []model.RideRequest{out}, newMemPool(av))
	if len(as) != 0 {
		t.Fatalf("AV must not leave the zone, got %+v", as)
	}
}

func TestGreedyRespectsRadius(t *testing.T) {
	pool := newMemPool(state.Candidate{VehicleID: "bronx", Kind: model.KindHuman, Lat: 40.8448, Lon: -73.8648})
	g := &Greedy{Travel: flatModel(), RadiusMi: 2, K: 5}
	as, _ := g.Dispatch(context.Background(), 0, []model.RideRequest{timesSq}, pool)
	if len(as) != 0 {
		t.Fatalf("vehicle beyond radius was assigned: %+v", as)
	}
}
