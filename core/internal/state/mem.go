package state

import (
	"context"
	"math"
	"sort"

	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
)

// Mem is an in-memory vehicle pool with the same dispatch-facing behavior as the Redis
// Store, used by offline (deterministic) runs. Idle vehicles are bucketed in a lat/lon
// grid so "nearest idle car" only looks at nearby cells instead of the whole fleet.
//
// Not safe for concurrent use: offline runs are single-threaded by design.
type Mem struct {
	cells map[cellKey]map[string]Candidate
	where map[string]cellKey // idle vehicle -> its cell
	seq   map[string]int64
	count map[model.VehicleKind]int
}

type cellKey struct{ lat, lon int32 }

// ~0.69 mi x ~0.68 mi at NYC's latitude.
const (
	cellDLat = 0.01
	cellDLon = 0.013
	cellMi   = 0.68
)

func keyOf(lat, lon float64) cellKey {
	return cellKey{int32(math.Floor(lat / cellDLat)), int32(math.Floor(lon / cellDLon))}
}

// NewMem returns an empty pool.
func NewMem() *Mem {
	return &Mem{
		cells: map[cellKey]map[string]Candidate{}, where: map[string]cellKey{},
		seq: map[string]int64{}, count: map[model.VehicleKind]int{},
	}
}

// AddIdle offers a vehicle to the dispatcher at a location.
func (m *Mem) AddIdle(id string, kind model.VehicleKind, lat, lon float64) {
	m.Remove(id)
	k := keyOf(lat, lon)
	if m.cells[k] == nil {
		m.cells[k] = map[string]Candidate{}
	}
	m.cells[k][id] = Candidate{VehicleID: id, Kind: kind, Lat: lat, Lon: lon}
	m.where[id] = k
	m.count[kind]++
}

// Remove takes a vehicle out of the idle pool (no-op if absent). Returns whether it was idle.
func (m *Mem) Remove(id string) bool {
	k, ok := m.where[id]
	if !ok {
		return false
	}
	m.count[m.cells[k][id].Kind]--
	delete(m.cells[k], id)
	if len(m.cells[k]) == 0 {
		delete(m.cells, k)
	}
	delete(m.where, id)
	return true
}

// IsIdle reports whether a vehicle is in the pool.
func (m *Mem) IsIdle(id string) bool { _, ok := m.where[id]; return ok }

// IdleCount returns the number of idle vehicles of a kind.
func (m *Mem) IdleCount(kind model.VehicleKind) int { return m.count[kind] }

// IdleIDs returns all idle vehicle IDs, sorted (deterministic).
func (m *Mem) IdleIDs() []string {
	ids := make([]string, 0, len(m.where))
	for id := range m.where {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// NearestIdle returns up to k idle vehicles of a kind within radiusMi, closest first.
// Ties are broken by ID so results never depend on Go's random map order.
func (m *Mem) NearestIdle(_ context.Context, kind model.VehicleKind, lat, lon, radiusMi float64, k int) ([]Candidate, error) {
	c := keyOf(lat, lon)
	r := int32(math.Ceil(radiusMi/cellMi)) + 1
	var out []Candidate
	for dl := -r; dl <= r; dl++ {
		for dn := -r; dn <= r; dn++ {
			for _, v := range m.cells[cellKey{c.lat + dl, c.lon + dn}] {
				if v.Kind != kind {
					continue
				}
				d := geo.HaversineMi(lat, lon, v.Lat, v.Lon)
				if d <= radiusMi {
					v.DistMi = d
					out = append(out, v)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DistMi != out[j].DistMi {
			return out[i].DistMi < out[j].DistMi
		}
		return out[i].VehicleID < out[j].VehicleID
	})
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

// NearestIdleMany implements dispatch.Pool.
func (m *Mem) NearestIdleMany(ctx context.Context, qs []Query) ([][]Candidate, error) {
	out := make([][]Candidate, len(qs))
	for i, q := range qs {
		out[i], _ = m.NearestIdle(ctx, q.Kind, q.Lat, q.Lon, q.RadiusMi, q.K)
	}
	return out, nil
}

// Claim implements dispatch.Pool: take an idle vehicle, returning its next sequence number.
func (m *Mem) Claim(_ context.Context, id string, _ model.VehicleKind) (int64, bool, error) {
	if !m.Remove(id) {
		return 0, false, nil
	}
	m.seq[id]++
	return m.seq[id], true, nil
}
