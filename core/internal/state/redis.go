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
	"errors"
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

// applyPings is applyPing looped over a chunk: KEYS = veh1, idle1, veh2, idle2, ...;
// ARGV = 8 values per ping in the same order. Returns one result per ping.
//
// Why: E1 showed Redis saturated at ~135k pings/s with one EVALSHA per ping costing
// ~14 us, while the commands inside it cost ~3 us. Most of the time was per-call script
// overhead, so one call per chunk amortizes it. Per-ping logic is identical to applyPing.
var applyPings = redis.NewScript(`
local out = {}
for i = 1, #KEYS / 2 do
  local vk, ik, o = KEYS[2*i-1], KEYS[2*i], (i-1)*8
  local ts = tonumber(ARGV[o+5])
  local cur = redis.call('HMGET', vk, 'ts', 'pending_seq')
  if ts <= (tonumber(cur[1]) or -1) then
    out[i] = 0
  else
    redis.call('HSET', vk, 'lat', ARGV[o+1], 'lon', ARGV[o+2], 'ts', ARGV[o+5],
               'kind', ARGV[o+6], 'cell', ARGV[o+7])
    if tonumber(ARGV[o+4]) < (tonumber(cur[2]) or 0) then
      out[i] = 2
    else
      redis.call('HSET', vk, 'status', ARGV[o+3], 'seq', ARGV[o+4])
      if ARGV[o+3] == 'idle' then
        redis.call('GEOADD', ik, ARGV[o+2], ARGV[o+1], ARGV[o+8])
      else
        redis.call('ZREM', ik, ARGV[o+8])
      end
      out[i] = 1
    end
  end
end
return out
`)

// pingChunk is how many pings go into one script call. Large enough to amortize the
// per-call overhead; small enough that one call doesn't block Redis for long (~1 ms).
const pingChunk = 256

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
type Store struct {
	rdb *redis.Client
	// PerPingScript selects the original one-Lua-call-per-ping ingest path (E1 A/B).
	PerPingScript bool
}

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
	if s.PerPingScript {
		return s.applyPingsPerPing(ctx, pings)
	}
	pipe := s.rdb.Pipeline()
	var cmds []*redis.Cmd
	for start := 0; start < len(pings); start += pingChunk {
		chunk := pings[start:min(start+pingChunk, len(pings))]
		keys := make([]string, 0, 2*len(chunk))
		args := make([]any, 0, 8*len(chunk))
		for _, p := range chunk {
			keys = append(keys, vehKey(p.VehicleID), idleKey(p.Kind))
			args = append(args, p.Lat, p.Lon, string(p.Status), p.Seq, p.TS, string(p.Kind),
				geo.Cell(p.Lat, p.Lon, geo.CellRes), p.VehicleID)
		}
		cmds = append(cmds, applyPings.EvalSha(ctx, pipe, keys, args...))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		if !redis.HasErrorPrefix(err, "NOSCRIPT") {
			return nil, err
		}
		// First call after Redis restarted: load the script, then retry once.
		if err := applyPings.Load(ctx, s.rdb).Err(); err != nil {
			return nil, err
		}
		return s.ApplyPings(ctx, pings)
	}
	out := make([]PingResult, 0, len(pings))
	for _, c := range cmds {
		vals, err := c.Int64Slice()
		if err != nil {
			return nil, err
		}
		for _, n := range vals {
			out = append(out, PingResult(n))
		}
	}
	return out, nil
}

// applyPingsPerPing is the original one-script-call-per-ping path, kept behind
// Store.PerPingScript so E1 can measure both on the same binary.
func (s *Store) applyPingsPerPing(ctx context.Context, pings []Ping) ([]PingResult, error) {
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
		if err := applyPing.Load(ctx, s.rdb).Err(); err != nil {
			return nil, err
		}
		return s.applyPingsPerPing(ctx, pings)
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

// Query is one nearest-idle search.
type Query struct {
	Kind     model.VehicleKind
	Lat, Lon float64
	RadiusMi float64
	K        int
}

// NearestIdleMany runs many searches in one pipeline: one network round trip instead of
// len(qs). With thousands of waiting riders per round this is the difference between a
// dispatch round taking milliseconds and taking hundreds of milliseconds.
func (s *Store) NearestIdleMany(ctx context.Context, qs []Query) ([][]Candidate, error) {
	if len(qs) == 0 {
		return nil, nil
	}
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.GeoSearchLocationCmd, len(qs))
	for i, q := range qs {
		cmds[i] = pipe.GeoSearchLocation(ctx, idleKey(q.Kind), &redis.GeoSearchLocationQuery{
			GeoSearchQuery: redis.GeoSearchQuery{
				Longitude: q.Lon, Latitude: q.Lat,
				Radius: q.RadiusMi, RadiusUnit: "mi",
				Sort: "ASC", Count: q.K,
			},
			WithCoord: true, WithDist: true,
		})
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	out := make([][]Candidate, len(qs))
	for i, c := range cmds {
		for _, r := range c.Val() {
			out[i] = append(out[i], Candidate{VehicleID: r.Name, Kind: qs[i].Kind,
				Lat: r.Latitude, Lon: r.Longitude, DistMi: r.Dist})
		}
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

// Snapshot reads every vehicle's position, status and kind (for the live dashboard).
// SCAN walks the keyspace in chunks without blocking Redis the way KEYS would; HMGETs
// are pipelined so 20k vehicles cost a handful of round trips.
func (s *Store) Snapshot(ctx context.Context) ([]Vehicle, error) {
	var keys []string
	iter := s.rdb.Scan(ctx, 0, "veh:*", 5000).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	out := make([]Vehicle, 0, len(keys))
	for start := 0; start < len(keys); start += 5000 {
		end := min(start+5000, len(keys))
		pipe := s.rdb.Pipeline()
		cmds := make([]*redis.SliceCmd, end-start)
		for i, k := range keys[start:end] {
			cmds[i] = pipe.HMGet(ctx, k, "lat", "lon", "status", "kind")
		}
		if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
			return nil, err
		}
		for i, c := range cmds {
			v := c.Val()
			if len(v) != 4 || v[0] == nil || v[1] == nil {
				continue
			}
			lat, _ := strconv.ParseFloat(fmt.Sprint(v[0]), 64)
			lon, _ := strconv.ParseFloat(fmt.Sprint(v[1]), 64)
			out = append(out, Vehicle{
				ID: keys[start+i][len("veh:"):], Lat: lat, Lon: lon,
				Status: model.VehicleStatus(fmt.Sprint(v[2])), Kind: model.VehicleKind(fmt.Sprint(v[3])),
			})
		}
	}
	return out, nil
}
