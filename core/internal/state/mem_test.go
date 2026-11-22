package state

import (
	"context"
	"math/rand/v2"
	"sort"
	"strconv"
	"testing"

	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
)

// The grid index must return exactly what a brute-force scan returns.
func TestMemMatchesBruteForce(t *testing.T) {
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(1, 2))
	m := NewMem()
	var all []Candidate
	for i := 0; i < 3000; i++ {
		c := Candidate{
			VehicleID: "v" + strconv.Itoa(i),
			Kind:      model.KindHuman,
			Lat:       40.6 + rng.Float64()*0.3, Lon: -74.05 + rng.Float64()*0.3,
		}
		m.AddIdle(c.VehicleID, c.Kind, c.Lat, c.Lon)
		all = append(all, c)
	}
	for q := 0; q < 200; q++ {
		lat, lon := 40.6+rng.Float64()*0.3, -74.05+rng.Float64()*0.3
		radius := 0.2 + rng.Float64()*3
		got, _ := m.NearestIdle(ctx, model.KindHuman, lat, lon, radius, 8)

		var want []Candidate
		for _, c := range all {
			if d := geo.HaversineMi(lat, lon, c.Lat, c.Lon); d <= radius {
				c.DistMi = d
				want = append(want, c)
			}
		}
		sort.Slice(want, func(i, j int) bool {
			if want[i].DistMi != want[j].DistMi {
				return want[i].DistMi < want[j].DistMi
			}
			return want[i].VehicleID < want[j].VehicleID
		})
		if len(want) > 8 {
			want = want[:8]
		}
		if len(got) != len(want) {
			t.Fatalf("query %d: got %d, want %d", q, len(got), len(want))
		}
		for i := range want {
			if got[i].VehicleID != want[i].VehicleID {
				t.Fatalf("query %d rank %d: got %s want %s", q, i, got[i].VehicleID, want[i].VehicleID)
			}
		}
	}
}

func TestMemClaimAndCounts(t *testing.T) {
	ctx := context.Background()
	m := NewMem()
	m.AddIdle("v1", model.KindAV, 40.75, -73.99)
	if m.IdleCount(model.KindAV) != 1 {
		t.Fatal("count after add")
	}
	seq, ok, _ := m.Claim(ctx, "v1", model.KindAV)
	if !ok || seq != 1 || m.IdleCount(model.KindAV) != 0 {
		t.Fatalf("claim: seq=%d ok=%v", seq, ok)
	}
	if _, ok, _ := m.Claim(ctx, "v1", model.KindAV); ok {
		t.Fatal("second claim must fail")
	}
	m.AddIdle("v1", model.KindAV, 40.76, -73.98)
	if seq, _, _ := m.Claim(ctx, "v1", model.KindAV); seq != 2 {
		t.Fatalf("seq should keep increasing, got %d", seq)
	}
}
