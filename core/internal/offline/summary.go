package offline

import (
	"sort"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/metrics"
)

// WaitStats describes a set of rider waits, in seconds.
type WaitStats struct {
	N    int     `json:"n"`
	Mean float64 `json:"mean_s"`
	P50  float64 `json:"p50_s"`
	P90  float64 `json:"p90_s"`
}

func waitStats(xs []float64) WaitStats {
	if len(xs) == 0 {
		return WaitStats{}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	sum := 0.0
	for _, x := range s {
		sum += x
	}
	return WaitStats{N: len(s), Mean: sum / float64(len(s)),
		P50: metrics.Quantile(s, 0.5), P90: metrics.Quantile(s, 0.9)}
}

// HourStats compares simulated and real waits for requests made in one hour.
type HourStats struct {
	Hour        int       `json:"hour"`
	Requests    int       `json:"requests"`
	Unserved    int       `json:"unserved"`
	Sim         WaitStats `json:"sim"`
	Real        WaitStats `json:"real"`
	MeanOnline  float64   `json:"mean_online"`
	Utilization float64   `json:"utilization"` // busy / online, simulated
}

// Summary is the headline result of a run over the measured window.
type Summary struct {
	Requests     int       `json:"requests"`
	Served       int       `json:"served"`
	Expired      int       `json:"expired"`
	Unfinished   int       `json:"unfinished"`
	UnservedRate float64   `json:"unserved_rate"`
	Sim          WaitStats `json:"sim_wait"`
	// Real is computed over the same requests (all of them, served or not in our sim),
	// so the comparison is apples to apples.
	Real       WaitStats      `json:"real_wait"`
	ByKind     map[string]int `json:"served_by_kind"`
	ByHour     []HourStats    `json:"by_hour"`
	Rounds     int            `json:"dispatch_rounds"`
	RuntimeSec float64        `json:"runtime_s"`
}

// Summarize computes stats over non-warm-up requests.
func Summarize(res *Result) Summary {
	s := Summary{ByKind: map[string]int{}, Rounds: res.Rounds, RuntimeSec: res.Elapsed.Seconds()}
	var sim, real []float64
	type hourAcc struct {
		req, unserved int
		sim, real     []float64
	}
	hours := map[int]*hourAcc{}
	for _, t := range res.Trips {
		if t.Warmup {
			continue
		}
		h := time.UnixMilli(t.RequestTS).UTC().Hour()
		if hours[h] == nil {
			hours[h] = &hourAcc{}
		}
		ha := hours[h]
		s.Requests++
		ha.req++
		real = append(real, float64(t.RealWaitS))
		ha.real = append(ha.real, float64(t.RealWaitS))
		switch t.Outcome {
		case Served:
			s.Served++
			s.ByKind[t.Kind]++
			sim = append(sim, t.WaitS)
			ha.sim = append(ha.sim, t.WaitS)
		case Expired:
			s.Expired++
			ha.unserved++
		case Unfinished:
			s.Unfinished++
			ha.unserved++
		}
	}
	if s.Requests > 0 {
		s.UnservedRate = float64(s.Expired+s.Unfinished) / float64(s.Requests)
	}
	s.Sim, s.Real = waitStats(sim), waitStats(real)

	type sampAcc struct{ online, busy, n float64 }
	samp := map[int]*sampAcc{}
	for _, x := range res.Samples {
		h := time.UnixMilli(x.TS).UTC().Hour()
		if samp[h] == nil {
			samp[h] = &sampAcc{}
		}
		samp[h].online += float64(x.Online)
		samp[h].busy += float64(x.Busy)
		samp[h].n++
	}
	for h := 0; h < 24; h++ {
		ha := hours[h]
		if ha == nil {
			continue
		}
		hs := HourStats{Hour: h, Requests: ha.req, Unserved: ha.unserved,
			Sim: waitStats(ha.sim), Real: waitStats(ha.real)}
		if sa := samp[h]; sa != nil && sa.online > 0 {
			hs.MeanOnline = sa.online / sa.n
			hs.Utilization = sa.busy / sa.online
		}
		s.ByHour = append(s.ByHour, hs)
	}
	return s
}
