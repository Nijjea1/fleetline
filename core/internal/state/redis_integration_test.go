//go:build integration

// Run with: make up && go test -tags integration ./internal/state/
package state

import (
	"context"
	"testing"

	"github.com/Nijjea1/fleetline/core/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := New("localhost:6379", 15) // DB 15: never collides with a running replay (DB 0)
	if err := s.Reset(context.Background()); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	t.Cleanup(func() { _ = s.Reset(context.Background()); _ = s.Close() })
	return s
}

func ping(id string, status model.VehicleStatus, seq, ts int64) Ping {
	return Ping{VehicleID: id, Kind: model.KindHuman, Lat: 40.75, Lon: -73.99, Status: status, Seq: seq, TS: ts}
}

func apply(t *testing.T, s *Store, p Ping) PingResult {
	t.Helper()
	r, err := s.ApplyPings(context.Background(), []Ping{p})
	if err != nil {
		t.Fatal(err)
	}
	return r[0]
}

func TestOutOfOrderPingIgnored(t *testing.T) {
	s := newTestStore(t)
	if r := apply(t, s, ping("v1", model.StatusIdle, 0, 2000)); r != PingApplied {
		t.Fatalf("first ping: %v", r)
	}
	if r := apply(t, s, ping("v1", model.StatusOnTrip, 0, 1000)); r != PingStale {
		t.Fatalf("older ping should be stale, got %v", r)
	}
	v, _ := s.Get(context.Background(), "v1")
	if v.Status != model.StatusIdle || v.TS != 2000 {
		t.Fatalf("state changed by stale ping: %+v", v)
	}
}

func TestDuplicatePingIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	p := ping("v1", model.StatusIdle, 0, 1000)
	apply(t, s, p)
	if r := apply(t, s, p); r != PingStale {
		t.Fatalf("redelivered ping should be ignored, got %v", r)
	}
}

// The race from D-008: the vehicle sent "idle" before it received its assignment.
// That ping must not put the vehicle back in the idle pool.
func TestPingBeforeAssignmentAckKeepsVehicleOutOfIdlePool(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	apply(t, s, ping("v1", model.StatusIdle, 0, 1000))

	seq, ok, err := s.Claim(ctx, "v1", model.KindHuman)
	if err != nil || !ok || seq != 1 {
		t.Fatalf("claim: seq=%d ok=%v err=%v", seq, ok, err)
	}
	if r := apply(t, s, ping("v1", model.StatusIdle, 0, 1500)); r != PingPositionOnly {
		t.Fatalf("pre-ack ping should be position-only, got %v", r)
	}
	if n, _ := s.IdleCount(ctx, model.KindHuman); n != 0 {
		t.Fatalf("vehicle leaked back into idle pool")
	}
	// Vehicle acknowledges seq 1 and reports en_route: full update.
	if r := apply(t, s, ping("v1", model.StatusEnRoute, 1, 2000)); r != PingApplied {
		t.Fatalf("acked ping should apply, got %v", r)
	}
	// Trip done, vehicle idle again with seq 1: back in the pool.
	apply(t, s, ping("v1", model.StatusIdle, 1, 9000))
	if n, _ := s.IdleCount(ctx, model.KindHuman); n != 1 {
		t.Fatalf("vehicle should be idle again")
	}
}

func TestClaimIsExclusive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	apply(t, s, ping("v1", model.StatusIdle, 0, 1000))
	_, ok1, _ := s.Claim(ctx, "v1", model.KindHuman)
	_, ok2, _ := s.Claim(ctx, "v1", model.KindHuman)
	if !ok1 || ok2 {
		t.Fatalf("exactly one claim should win: %v %v", ok1, ok2)
	}
}

func TestNearestIdleOrdersByDistance(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	far := Ping{VehicleID: "far", Kind: model.KindHuman, Lat: 40.80, Lon: -73.95, Status: model.StatusIdle, TS: 1}
	near := Ping{VehicleID: "near", Kind: model.KindHuman, Lat: 40.751, Lon: -73.99, Status: model.StatusIdle, TS: 1}
	av := Ping{VehicleID: "av1", Kind: model.KindAV, Lat: 40.750, Lon: -73.99, Status: model.StatusIdle, TS: 1}
	if _, err := s.ApplyPings(ctx, []Ping{far, near, av}); err != nil {
		t.Fatal(err)
	}
	c, err := s.NearestIdle(ctx, model.KindHuman, 40.75, -73.99, 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 2 || c[0].VehicleID != "near" || c[1].VehicleID != "far" {
		t.Fatalf("unexpected candidates %+v", c)
	}
}

func TestNearestIdleManyMatchesSingle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	ps := []Ping{
		{VehicleID: "a", Kind: model.KindHuman, Lat: 40.751, Lon: -73.99, Status: model.StatusIdle, TS: 1},
		{VehicleID: "b", Kind: model.KindHuman, Lat: 40.80, Lon: -73.95, Status: model.StatusIdle, TS: 1},
	}
	if _, err := s.ApplyPings(ctx, ps); err != nil {
		t.Fatal(err)
	}
	qs := []Query{
		{Kind: model.KindHuman, Lat: 40.75, Lon: -73.99, RadiusMi: 10, K: 5},
		{Kind: model.KindHuman, Lat: 40.80, Lon: -73.95, RadiusMi: 0.1, K: 5},
		{Kind: model.KindAV, Lat: 40.75, Lon: -73.99, RadiusMi: 10, K: 5}, // empty set
	}
	got, err := s.NearestIdleMany(ctx, qs)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range qs {
		want, _ := s.NearestIdle(ctx, q.Kind, q.Lat, q.Lon, q.RadiusMi, q.K)
		if len(got[i]) != len(want) {
			t.Fatalf("query %d: batch %d results, single %d", i, len(got[i]), len(want))
		}
		for j := range want {
			if got[i][j].VehicleID != want[j].VehicleID {
				t.Fatalf("query %d result %d differs", i, j)
			}
		}
	}
}
