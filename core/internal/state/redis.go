// Package state keeps live vehicle state in Redis.
//
// Layout:
//
//	veh:<id>      hash: lat, lon, status, kind, cell, ts, seq, pending_seq
//	idle:<kind>   geo set of idle vehicles (GEOSEARCH answers "nearest idle car")
//
// All writes go through Lua scripts. Redis runs a script atomically: no other command
// interleaves, so "read current state, decide, write" can't race (D-008).
package state

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
)

// PingResult says what ApplyPing did with a ping.
type PingResult int

const (
	PingStale        PingResult = 0 // older than stored state: ignored
	PingApplied      PingResult = 1 // position + status updated
	PingPositionOnly PingResult = 2 // vehicle hasn't seen its latest assignment: status kept
)

// applyPing: KEYS = veh hash, idle geo set. ARGV = lat, lon, status, seq, ts, kind, cell, id
var applyPing = redis.NewScript(`
local ts = tonumber(ARGV[5])
local cur = redis.call('HMGET', KEYS[1], 'ts', 'pending_seq')
if ts <= (tonumber(cur[1]) or -1) then
  return 0
end
redis.call('HSET', KEYS[1], 'lat', ARGV[1], 'lon', ARGV[2], 'ts', ARGV[5],
           'kind', ARGV[6], 'cell', ARGV[7])
if tonumber(ARGV[4]) < (tonumber(cur[2]) or 0) then
  return 2
end
redis.call('HSET', KEYS[1], 'status', ARGV[3], 'seq', ARGV[4])
if ARGV[3] == 'idle' then
  redis.call('GEOADD', KEYS[2], ARGV[2], ARGV[1], ARGV[8])
else
  redis.call('ZREM', KEYS[2], ARGV[8])
end
return 1
`)

// claim: KEYS = veh hash, idle geo set. ARGV = id. Returns new seq, or -1 if not idle.
var claim = redis.NewScript(`
if not redis.call('ZSCORE', KEYS[2], ARGV[1]) then
  return -1
end
redis.call('ZREM', KEYS[2], ARGV[1])
local seq = redis.call('HINCRBY', KEYS[1], 'pending_seq', 1)
redis.call('HSET', KEYS[1], 'status', 'en_route')
return seq
`)

// Store is the Redis-backed vehicle state.
type Store struct{ rdb *redis.Client }

// New connects to Redis at addr, using database db.
func New(addr string, db int) *Store {
	return &Store{rdb: redis.NewClient(&redis.Options{Addr: addr, DB: db, PoolSize: 32})}
}

func vehKey(id string) string            { return "veh:" + id }
func idleKey(k model.VehicleKind) string { return "idle:" + string(k) }

// Ping is a convenience alias so callers don't need two imports.
type Ping = model.Ping

// ApplyPings writes a batch of pings in one network round trip (a Redis pipeline).
// Batching is the main lever for ingest throughput; E1 measures it.
func (s *Store) ApplyPings(ctx context.Context, pings []Ping) ([]PingResult, error) {
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.Cmd, len(pings))
	for i, p := range pings {
		cmds[i] = applyPing.EvalSha(ctx, pipe,
			[]string{vehKey(p.VehicleID), idleKey(p.Kind)},
			p.Lat, p.Lon, string(p.Status), p.Seq, p.TS, string(p.Kind),
			geo.Cell(p.Lat, p.Lon, geo.CellRes), p.VehicleID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		if !redis.HasErrorPrefix(err, "NOSCRIPT") {
			return nil, err
		}
		// First call after Redis restarted: load the script, then retry once.
		if err := applyPing.Load(ctx, s.rdb).Err(); err != nil {
			return nil, err
		}
		return s.ApplyPings(ctx, pings)
	}
	out := make([]PingResult, len(pings))
	for i, c := range cmds {
		n, err := c.Int()
		if err != nil {
			return nil, err
		}
		out[i] = PingResult(n)
	}
	return out, nil
}

// Candidate is an idle vehicle near a point.
type Candidate struct {
	VehicleID string
	Kind      model.VehicleKind
	Lat, Lon  float64
	DistMi    float64 // straight-line distance
}

// NearestIdle returns up to k idle vehicles of a kind within radiusMi, closest first.
func (s *Store) NearestIdle(ctx context.Context, kind model.VehicleKind, lat, lon, radiusMi float64, k int) ([]Candidate, error) {
	res, err := s.rdb.GeoSearchLocation(ctx, idleKey(kind), &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude: lon, Latitude: lat,
			Radius: radiusMi, RadiusUnit: "mi",
			Sort: "ASC", Count: k,
		},
		WithCoord: true, WithDist: true,
	}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, len(res))
	for i, r := range res {
		out[i] = Candidate{VehicleID: r.Name, Kind: kind, Lat: r.Latitude, Lon: r.Longitude, DistMi: r.Dist}
	}
	return out, nil
}

// Claim atomically takes an idle vehicle for an assignment. ok=false means another
// decision got it first (or it stopped being idle); the caller should try the next one.
func (s *Store) Claim(ctx context.Context, vehicleID string, kind model.VehicleKind) (seq int64, ok bool, err error) {
	n, err := claim.Run(ctx, s.rdb, []string{vehKey(vehicleID), idleKey(kind)}, vehicleID).Int64()
	if err != nil {
		return 0, false, err
	}
	return n, n > 0, nil
}

// Vehicle is a full state snapshot of one vehicle.
type Vehicle struct {
	ID         string
	Kind       model.VehicleKind
	Status     model.VehicleStatus
	Lat, Lon   float64
	TS         int64
	Seq        int64
	PendingSeq int64
}

// Get reads one vehicle's state.
func (s *Store) Get(ctx context.Context, id string) (Vehicle, error) {
	m, err := s.rdb.HGetAll(ctx, vehKey(id)).Result()
	if err != nil {
		return Vehicle{}, err
	}
	if len(m) == 0 {
		return Vehicle{}, fmt.Errorf("vehicle %s not found", id)
	}
	f := func(k string) float64 { v, _ := strconv.ParseFloat(m[k], 64); return v }
	i := func(k string) int64 { v, _ := strconv.ParseInt(m[k], 10, 64); return v }
	return Vehicle{
		ID: id, Kind: model.VehicleKind(m["kind"]), Status: model.VehicleStatus(m["status"]),
		Lat: f("lat"), Lon: f("lon"), TS: i("ts"), Seq: i("seq"), PendingSeq: i("pending_seq"),
	}, nil
}

// IdleCount returns how many vehicles of a kind are idle.
func (s *Store) IdleCount(ctx context.Context, kind model.VehicleKind) (int64, error) {
	return s.rdb.ZCard(ctx, idleKey(kind)).Result()
}

// Reset deletes all Fleetline state (used at the start of a replay and in tests).
func (s *Store) Reset(ctx context.Context) error { return s.rdb.FlushDB(ctx).Err() }

// Ping checks connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.rdb.Ping(ctx).Err() }

// Close closes the connection pool.
func (s *Store) Close() error { return s.rdb.Close() }
