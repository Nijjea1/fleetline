package world

import (
	"testing"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
)

// 10 mph everywhere, no detour: 1 mile takes exactly 360 s.
func testCfg() Config {
	var tm geo.TravelModel
	tm.DetourFactor = 1
	for h := range tm.SpeedMPHByHour {
		tm.SpeedMPHByHour[h] = 10
	}
	return Config{Travel: tm, PingInterval: 4 * time.Second, Patience: 10 * time.Minute}
}

const mileLat = 1.0 / 69.05 // ~1 mile of latitude

func TestFullTripLifecycle(t *testing.T) {
	v := &Vehicle{ID: "h1", Kind: model.KindHuman, ToLat: 40.0, ToLon: -74.0}
	w := New(testCfg(), 0, []*Vehicle{v})
	w.Request(model.RideRequest{TripID: 7, PickLat: 40 + mileLat, PickLon: -74, DropLat: 40, DropLon: -74, TripTime: 600, TS: 0})
	if err := w.Assign(model.Assignment{TripID: 7, VehicleID: "h1", Seq: 1}); err != nil {
		t.Fatal(err)
	}

	// Halfway to pickup after 180 s.
	w.Advance(180_000)
	lat, _ := v.Position(w.Now())
	if d := (lat - 40) / mileLat; d < 0.49 || d > 0.51 {
		t.Fatalf("expected halfway, got %.3f miles", d)
	}

	// Arrives at ~360 s, rides 600 s, drops off at ~960 s.
	st := w.Advance(2_000_000)
	if len(st.Events) != 2 || st.Events[0].Type != model.TripPickup || st.Events[1].Type != model.TripDropoff {
		t.Fatalf("unexpected events %+v", st.Events)
	}
	if pu := st.Events[0].TS; pu < 359_000 || pu > 361_000 {
		t.Fatalf("pickup at %d ms, want ~360000", pu)
	}
	if do := st.Events[1].TS - st.Events[0].TS; do != 600_000 {
		t.Fatalf("ride took %d ms, want the real 600000", do)
	}
	if v.Status != model.StatusIdle || v.Seq != 1 {
		t.Fatalf("vehicle should be idle with seq 1: %+v", v)
	}
}

func TestRiderExpiresWithoutAssignment(t *testing.T) {
	w := New(testCfg(), 0, nil)
	w.Request(model.RideRequest{TripID: 1, TS: 0})
	if ev := w.Advance(9 * 60_000).Events; len(ev) != 0 {
		t.Fatalf("expired too early: %+v", ev)
	}
	ev := w.Advance(11 * 60_000).Events
	if len(ev) != 1 || ev[0].Type != model.TripExpired || ev[0].TS != 600_000 {
		t.Fatalf("want one expiry at 600000, got %+v", ev)
	}
}

func TestLateAssignmentStillAdvancesSeq(t *testing.T) {
	v := &Vehicle{ID: "h1", ToLat: 40, ToLon: -74}
	w := New(testCfg(), 0, []*Vehicle{v})
	w.Request(model.RideRequest{TripID: 1, TS: 0})
	w.Advance(11 * 60_000) // rider gives up
	if err := w.Assign(model.Assignment{TripID: 1, VehicleID: "h1", Seq: 3}); err == nil {
		t.Fatal("assignment to an expired trip should fail")
	}
	// Vehicle must still ack seq 3, or Redis would keep it out of the idle pool forever.
	if v.Seq != 3 || v.Status != model.StatusIdle {
		t.Fatalf("got seq %d status %s", v.Seq, v.Status)
	}
}

func TestPingsAreStaggeredAndPeriodic(t *testing.T) {
	vs := SpawnFleet(100, 0.1, []Point{{Lat: 40, Lon: -74}}, 1)
	w := New(testCfg(), 0, vs)
	total := 0
	for ts := int64(0); ts < 40_000; ts += 1000 {
		total += len(w.Advance(ts).Pings)
	}
	// 100 vehicles x (40 s / 4 s) = 1000 pings.
	if total != 1000 {
		t.Fatalf("got %d pings, want 1000", total)
	}
}

func TestSpawnFleetDeterministicWithAVShare(t *testing.T) {
	reqs := []Point{{Lat: 1}, {Lat: 2}, {Lat: 3}}
	a := SpawnFleet(50, 0.2, reqs, 42)
	b := SpawnFleet(50, 0.2, reqs, 42)
	av := 0
	for i := range a {
		if a[i].ToLat != b[i].ToLat {
			t.Fatal("same seed must place vehicles identically")
		}
		if a[i].Kind == model.KindAV {
			av++
		}
	}
	if av != 10 {
		t.Fatalf("want 10 AVs, got %d", av)
	}
}

func TestEventsInGlobalTimeOrderAcrossVehicles(t *testing.T) {
	// A: pickup at 360 s, ride 100 s -> dropoff at 460 s.
	// B: pickup at 360 s too, but its ride is 50 s -> dropoff at 410 s.
	// One big Advance must interleave them by time, not vehicle by vehicle.
	a := &Vehicle{ID: "a", ToLat: 40, ToLon: -74}
	b := &Vehicle{ID: "b", ToLat: 40, ToLon: -74}
	w := New(testCfg(), 0, []*Vehicle{a, b})
	w.Request(model.RideRequest{TripID: 1, PickLat: 40 + mileLat, PickLon: -74, DropLat: 40, DropLon: -74, TripTime: 100})
	w.Request(model.RideRequest{TripID: 2, PickLat: 40 + mileLat, PickLon: -74, DropLat: 40, DropLon: -74, TripTime: 50})
	_ = w.Assign(model.Assignment{TripID: 1, VehicleID: "a", Seq: 1})
	_ = w.Assign(model.Assignment{TripID: 2, VehicleID: "b", Seq: 1})
	ev := w.Advance(1_000_000).Events
	if len(ev) != 4 {
		t.Fatalf("want 4 events, got %+v", ev)
	}
	for i := 1; i < len(ev); i++ {
		if ev[i].TS < ev[i-1].TS {
			t.Fatalf("events out of order: %+v", ev)
		}
	}
	if ev[2].TripID != 2 || ev[2].Type != model.TripDropoff {
		t.Fatalf("B's earlier dropoff should come third: %+v", ev)
	}
}

func TestOfflineModeEmitsNoPings(t *testing.T) {
	cfg := testCfg()
	cfg.PingInterval = 0
	w := New(cfg, 0, SpawnFleet(10, 0, []Point{{Lat: 40, Lon: -74}}, 1))
	if p := w.Advance(60_000).Pings; len(p) != 0 {
		t.Fatalf("offline mode should not emit pings, got %d", len(p))
	}
}
