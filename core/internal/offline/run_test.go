package offline

import (
	"context"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/dispatch"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/replay"
)

func flatTravel() geo.TravelModel {
	var m geo.TravelModel
	m.DetourFactor = 1.3
	for h := range m.SpeedMPHByHour {
		m.SpeedMPHByHour[h] = 12
	}
	return m
}

// synthetic: n requests spread over 07:00-08:00 in a ~3x3 mile Manhattan box.
func synthetic(n int, seed uint64) []replay.Trip {
	rng := rand.New(rand.NewPCG(seed, 1))
	start := at(6, 0)
	out := make([]replay.Trip, n)
	for i := range out {
		out[i] = replay.Trip{RideRequest: model.RideRequest{
			TripID: int64(i), TS: start + int64(i)*(2*3_600_000)/int64(n),
			PickLat: 40.73 + rng.Float64()*0.04, PickLon: -74.00 + rng.Float64()*0.04,
			DropLat: 40.73 + rng.Float64()*0.04, DropLon: -74.00 + rng.Float64()*0.04,
			TripTime: 600 + rng.Int64N(900),
		}, RealWaitS: 200}
	}
	return out
}

func cfg(fleet int) Config {
	tm := flatTravel()
	return Config{
		Travel: tm, Policy: &dispatch.Greedy{Travel: tm, RadiusMi: 3, K: 8},
		From: at(7, 0), To: at(8, 0), Warmup: 30 * time.Minute, Drain: 30 * time.Minute,
		Patience: 10 * time.Minute, Window: time.Second, Supply: FixedSupply(fleet),
		AVShare: 0.2, Seed: 7,
	}
}

func TestDeterministic(t *testing.T) {
	trips := synthetic(2000, 1)
	a, err := Run(context.Background(), cfg(150), trips)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Run(context.Background(), cfg(150), trips)
	if !reflect.DeepEqual(a.Trips, b.Trips) || !reflect.DeepEqual(a.Samples, b.Samples) {
		t.Fatal("same config and seed must give identical results")
	}
}

func TestEveryRequestHasOneOutcome(t *testing.T) {
	trips := synthetic(2000, 2)
	res, err := Run(context.Background(), cfg(150), trips)
	if err != nil {
		t.Fatal(err)
	}
	// Requests from 06:30 (warm-up start) to 08:00.
	want := 0
	for _, tr := range trips {
		if tr.TS >= at(6, 30) && tr.TS < at(8, 0) {
			want++
		}
	}
	if len(res.Trips) != want {
		t.Fatalf("got %d results, want %d", len(res.Trips), want)
	}
	for _, tr := range res.Trips {
		switch tr.Outcome {
		case Served:
			if tr.WaitS <= 0 || tr.Kind == "" {
				t.Fatalf("served trip missing wait or kind: %+v", tr)
			}
		case Expired, Unfinished:
		default:
			t.Fatalf("bad outcome %+v", tr)
		}
		if tr.Warmup != (tr.RequestTS < at(7, 0)) {
			t.Fatalf("warm-up flag wrong: %+v", tr)
		}
	}
}

func TestMoreSupplyMeansShorterWaits(t *testing.T) {
	trips := synthetic(2000, 3)
	mean := func(fleet int) (float64, int) {
		res, err := Run(context.Background(), cfg(fleet), trips)
		if err != nil {
			t.Fatal(err)
		}
		sum, n, expired := 0.0, 0, 0
		for _, tr := range res.Trips {
			if tr.Warmup {
				continue
			}
			if tr.Outcome == Served {
				sum += tr.WaitS
				n++
			} else {
				expired++
			}
		}
		return sum / float64(n), expired
	}
	small, expSmall := mean(80)
	big, expBig := mean(300)
	if big >= small || expBig > expSmall {
		t.Fatalf("supply 300 should beat 80: wait %.0f vs %.0f, expired %d vs %d", big, small, expBig, expSmall)
	}
}

func TestDataSupplyTracksTarget(t *testing.T) {
	c := cfg(0)
	var d DataSupply
	d.Utilization = 1
	for h := range d.Busy {
		d.Busy[h] = 100
	}
	d.Busy[7] = 300 // ramp up toward 07:30
	c.Supply = d
	res, err := Run(context.Background(), c, synthetic(2000, 4))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Samples {
		// Spawning is immediate, so online can only exceed target while busy drivers
		// finish their trip before logging off.
		if s.Online < s.Target {
			t.Fatalf("supply below target at %v: %+v", time.UnixMilli(s.TS).UTC(), s)
		}
	}
}

// Regression: with no trips in the hour before the start, every car used to spawn on the
// same point, making nearest-car search scan the whole fleet in one grid cell.
func TestStartOfDaySpawnIsSpreadOut(t *testing.T) {
	trips := synthetic(2000, 5) // first trip at 06:00
	c := cfg(200)
	c.From, c.Warmup = at(6, 0), 0
	r := &runner{cfg: c, trips: trips, results: map[int64]*TripResult{}, online: map[string]bool{}}
	r.rng = rand.New(rand.NewPCG(1, 2))
	r.pool = newPool()
	r.w = newWorld(c, c.From)
	for i := 0; i < 200; i++ {
		r.spawn(c.From)
	}
	seen := map[[2]float64]bool{}
	for _, v := range r.w.Vehicles() {
		seen[[2]float64{v.ToLat, v.ToLon}] = true
	}
	if len(seen) < 150 {
		t.Fatalf("only %d distinct spawn points for 200 cars", len(seen))
	}
}
