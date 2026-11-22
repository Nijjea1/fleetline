package dispatch

import (
	"context"
	"reflect"
	"sort"
	"strconv"
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

func (p *memPool) NearestIdleMany(ctx context.Context, qs []state.Query) ([][]state.Candidate, error) {
	out := make([][]state.Candidate, len(qs))
	for i, q := range qs {
		out[i], _ = p.NearestIdle(ctx, q.Kind, q.Lat, q.Lon, q.RadiusMi, q.K)
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

func TestGreedyPickupCap(t *testing.T) {
	// ~1.3 road miles at 10 mph = ~470 s away.
	far := state.Candidate{VehicleID: "far", Kind: model.KindHuman, Lat: 40.7580 + 1.0/69.05, Lon: -73.9855}
	g := &Greedy{Travel: flatModel(), RadiusMi: 3, K: 5, MaxPickupSec: 300}
	if as, _ := g.Dispatch(context.Background(), 0, []model.RideRequest{timesSq}, newMemPool(far)); len(as) != 0 {
		t.Fatalf("pickup over the cap was assigned: %+v", as)
	}
	g.MaxPickupSec = 600
	if as, _ := g.Dispatch(context.Background(), 0, []model.RideRequest{timesSq}, newMemPool(far)); len(as) != 1 {
		t.Fatal("pickup under the cap should be assigned")
	}
}

// hidden wraps a pool so the ChangeTracker capability is invisible to the policy.
type hidden struct{ Pool }

// The skip-unchanged optimization must never change a decision. Replay many rounds with
// vehicles arriving over time, once with change tracking and once without.
func TestSkipUnchangedGivesIdenticalDecisions(t *testing.T) {
	run := func(track bool) []model.Assignment {
		mem := state.NewMem()
		var pool Pool = mem
		if !track {
			pool = hidden{mem}
		}
		g := &Greedy{Travel: flatModel(), RadiusMi: 3, K: 8, MaxPickupSec: 420}
		var all []model.Assignment
		var pending []model.RideRequest
		for round := 0; round < 300; round++ {
			// a new rider every round, a new idle car every third round, spread over Manhattan
			f := float64(round%17) / 17
			pending = append(pending, model.RideRequest{TripID: int64(round),
				PickLat: 40.72 + 0.06*f, PickLon: -74.00 + 0.03*f, DropLat: 40.75, DropLon: -73.99})
			if round%3 == 0 {
				h := float64((round*7)%23) / 23
				mem.AddIdle("v"+strconv.Itoa(round), model.KindHuman, 40.72+0.06*h, -74.0+0.03*h)
			}
			as, err := g.Dispatch(context.Background(), int64(round)*1000, pending, pool)
			if err != nil {
				t.Fatal(err)
			}
			done := map[int64]bool{}
			for _, a := range as {
				done[a.TripID] = true
			}
			left := pending[:0]
			for _, r := range pending {
				if !done[r.TripID] {
					left = append(left, r)
				}
			}
			pending = left
			all = append(all, as...)
		}
		return all
	}
	a, b := run(true), run(false)
	if len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Fatalf("tracking changed decisions: %d vs %d assignments", len(a), len(b))
	}
}

// Regression: more idle cars than K near a crowd of riders. A rider whose K nearest cars
// were all taken earlier in the same round must be retried next round even though no new
// car appeared: an idle car beyond its top K still exists. The full-day A/B caught this.
func TestSkipUnchangedRespectsTopKTruncation(t *testing.T) {
	run := func(track bool) []model.Assignment {
		mem := state.NewMem()
		var pool Pool = mem
		if !track {
			pool = hidden{mem}
		}
		for i := 0; i < 6; i++ { // 6 cars around Times Square
			mem.AddIdle("v"+strconv.Itoa(i), model.KindHuman, 40.7580+float64(i)*0.0005, -73.9855)
		}
		g := &Greedy{Travel: flatModel(), RadiusMi: 3, K: 2, MaxPickupSec: 900}
		var pending []model.RideRequest
		for i := 0; i < 6; i++ {
			pending = append(pending, model.RideRequest{TripID: int64(i), PickLat: 40.7580, PickLon: -73.9855,
				DropLat: 40.75, DropLon: -73.99})
		}
		var all []model.Assignment
		for round := 0; round < 5; round++ {
			as, _ := g.Dispatch(context.Background(), int64(round)*1000, pending, pool)
			done := map[int64]bool{}
			for _, a := range as {
				done[a.TripID] = true
			}
			left := pending[:0]
			for _, r := range pending {
				if !done[r.TripID] {
					left = append(left, r)
				}
			}
			pending = left
			all = append(all, as...)
		}
		return all
	}
	a, b := run(true), run(false)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("tracking changed decisions: %d vs %d assignments", len(a), len(b))
	}
	if len(b) != 6 {
		t.Fatalf("all 6 riders should eventually get one of the 6 cars, got %d", len(b))
	}
}
