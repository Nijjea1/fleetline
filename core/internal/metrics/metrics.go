// Package metrics defines fleetd's Prometheus metrics and an exact in-memory summary of
// rider outcomes. Prometheus histograms are approximate (fixed buckets), fine for live
// graphs; benchmark numbers come from Summary, which keeps every value.
package metrics

import (
	"sort"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	PingsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetline_pings_total", Help: "Vehicle pings processed, by result.",
	}, []string{"result"})

	IngestLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "fleetline_ingest_latency_seconds",
		Help: "Wall time from broker append to the ping being applied in Redis.",
		// 0.5 ms .. ~33 s, doubling (wide enough to see a saturated consumer).
		Buckets: prometheus.ExponentialBuckets(0.0005, 2, 17),
	})

	PingBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "fleetline_ping_batch_size", Help: "Pings written per Redis pipeline.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 14),
	})

	RequestsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "fleetline_requests_total", Help: "Ride requests received.",
	})

	AssignmentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetline_assignments_total", Help: "Assignments made, by vehicle kind.",
	}, []string{"kind"})

	DispatchRound = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "fleetline_dispatch_round_seconds", Help: "Wall time of one dispatch round.",
		Buckets: prometheus.ExponentialBuckets(0.0001, 2, 16),
	})

	Pending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "fleetline_pending_requests", Help: "Requests waiting for a vehicle.",
	})

	Watermark = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "fleetline_watermark_ms", Help: "Latest event time seen (simulated clock).",
	})

	RiderWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "fleetline_rider_wait_seconds", Help: "Simulated request-to-pickup wait.",
		Buckets: []float64{30, 60, 120, 180, 240, 300, 420, 600, 900},
	})

	TripsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetline_trips_total", Help: "Trip outcomes reported by the world.",
	}, []string{"outcome"})
)

// Summary keeps exact rider outcomes for benchmark reporting.
type Summary struct {
	mu      sync.Mutex
	waits   []float64
	expired int
	byKind  map[string]int
}

// NewSummary returns an empty summary.
func NewSummary() *Summary { return &Summary{byKind: map[string]int{}} }

// Pickup records a served rider's wait in seconds.
func (s *Summary) Pickup(waitSec float64) {
	s.mu.Lock()
	s.waits = append(s.waits, waitSec)
	s.mu.Unlock()
}

// Expired records a rider who gave up.
func (s *Summary) Expired() { s.mu.Lock(); s.expired++; s.mu.Unlock() }

// Assigned records an assignment by vehicle kind.
func (s *Summary) Assigned(kind string) { s.mu.Lock(); s.byKind[kind]++; s.mu.Unlock() }

// Snapshot is a JSON-friendly view of the summary.
type Snapshot struct {
	Served         int            `json:"served"`
	Expired        int            `json:"expired"`
	UnservedRate   float64        `json:"unserved_rate"`
	WaitMeanSec    float64        `json:"wait_mean_s"`
	WaitP50Sec     float64        `json:"wait_p50_s"`
	WaitP90Sec     float64        `json:"wait_p90_s"`
	AssignedByKind map[string]int `json:"assigned_by_kind"`
}

// Snapshot computes exact statistics over everything recorded so far.
func (s *Summary) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := append([]float64(nil), s.waits...)
	sort.Float64s(w)
	snap := Snapshot{Served: len(w), Expired: s.expired, AssignedByKind: map[string]int{}}
	for k, v := range s.byKind {
		snap.AssignedByKind[k] = v
	}
	if total := len(w) + s.expired; total > 0 {
		snap.UnservedRate = float64(s.expired) / float64(total)
	}
	if len(w) > 0 {
		sum := 0.0
		for _, x := range w {
			sum += x
		}
		snap.WaitMeanSec = sum / float64(len(w))
		snap.WaitP50Sec = Quantile(w, 0.5)
		snap.WaitP90Sec = Quantile(w, 0.9)
	}
	return snap
}

// Quantile of sorted data using linear interpolation (same as numpy's default).
func Quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lo := int(pos)
	if lo+1 >= len(sorted) {
		return sorted[lo]
	}
	return sorted[lo] + (pos-float64(lo))*(sorted[lo+1]-sorted[lo])
}
