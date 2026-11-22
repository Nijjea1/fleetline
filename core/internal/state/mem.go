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
	cells map[model.VehicleKind]map[cellKey]map[string]Candidate // indexed per kind
	where map[string]cellKey                                     // idle vehicle -> its cell
	kind  map[string]model.VehicleKind
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
		cells: map[model.VehicleKind]map[cellKey]map[string]Candidate{},
		where: map[string]cellKey{}, kind: map[string]model.VehicleKind{},
		seq: map[string]int64{}, count: map[model.VehicleKind]int{},
	}
}

// AddIdle offers a vehicle to the dispatcher at a location.
func (m *Mem) AddIdle(id string, kind model.VehicleKind, lat, lon float64) {
	m.Remove(id)
	k := keyOf(lat, lon)
	if m.cells[kind] == nil {
		m.cells[kind] = map[cellKey]map[string]Candidate{}
	}
	if m.cells[kind][k] == nil {
		m.cells[kind][k] = map[string]Candidate{}
	}
	m.cells[kind][k][id] = Candidate{VehicleID: id, Kind: kind, Lat: lat, Lon: lon}
	m.where[id] = k
	m.kind[id] = kind
	m.count[kind]++
}

// Remove takes a vehicle out of the idle pool (no-op if absent). Returns whether it was idle.
func (m *Mem) Remove(id string) bool {
	k, ok := m.where[id]
	if !ok {
		return false
	}
	kind := m.kind[id]
	m.count[kind]--
	delete(m.cells[kind][k], id)
	if len(m.cells[kind][k]) == 0 {
		delete(m.cells[kind], k)
	}
	delete(m.where, id)
	delete(m.kind, id)
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
	if m.count[kind] == 0 {
		return nil, nil
	}
	cells := m.cells[kind]
	c := keyOf(lat, lon)
	maxRing := int32(math.Ceil(radiusMi/cellMi)) + 1
	var out []Candidate
	visit := func(key cellKey) {
		for _, v := range cells[key] {
			if d := geo.HaversineMi(lat, lon, v.Lat, v.Lon); d <= radiusMi {
				v.DistMi = d
				out = append(out, v)
			}
		}
	}
	// Search outward one square ring of cells at a time. The query point sits inside the
	// center cell, so every vehicle in ring d+1 is at least d*cellMi away. Once we hold k
	// vehicles that are all closer than that, no farther ring can change the answer.
	for d := int32(0); d <= maxRing; d++ {
		if d == 0 {
			visit(c)
		} else {
			for i := -d; i <= d; i++ {
				visit(cellKey{c.lat - d, c.lon + i})
				visit(cellKey{c.lat + d, c.lon + i})
			}
			for i := -d + 1; i <= d-1; i++ {
				visit(cellKey{c.lat + i, c.lon - d})
				visit(cellKey{c.lat + i, c.lon + d})
			}
		}
		if len(out) >= k {
			sortCandidates(out)
			if out[k-1].DistMi <= float64(d)*cellMi {
				break
			}
		}
	}
	sortCandidates(out)
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

// sortCandidates orders by distance, then ID so results never depend on map order.
func sortCandidates(cs []Candidate) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].DistMi != cs[j].DistMi {
			return cs[i].DistMi < cs[j].DistMi
		}
		return cs[i].VehicleID < cs[j].VehicleID
	})
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
