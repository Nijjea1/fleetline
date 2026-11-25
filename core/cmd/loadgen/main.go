// Command loadgen is the E1 load test: it offers vehicle pings to Kafka at fixed rates
// and measures what fleetd actually ingests, scraping fleetd's Prometheus metrics.
//
// Open loop: pings are sent on a fixed schedule regardless of how fleetd is doing, so a
// slow consumer shows up as growing lag and latency (a closed loop would hide it by
// slowing the sender down). No ride requests are sent, so dispatch adds no noise.
//
// Per step it records: offered rate actually produced, rate fleetd applied to Redis,
// ingest latency quantiles (broker append -> Redis write, worst ping per batch), lag at
// the end of the step, and how long the backlog took to drain afterwards.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/bus"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
)

type Step struct {
	Rate        int     `json:"offered_rate"`
	Rep         int     `json:"rep"`
	Produced    float64 `json:"produced_per_s"`
	Applied     float64 `json:"applied_per_s"`
	P50ms       float64 `json:"ingest_p50_ms"`
	P99ms       float64 `json:"ingest_p99_ms"`
	MaxBucketMs float64 `json:"ingest_max_bucket_ms"`
	LagEnd      int64   `json:"lag_end"`
	DrainSec    float64 `json:"drain_s"`
	MeanBatch   float64 `json:"mean_batch"`
	Stale       float64 `json:"stale_frac"`
	Sustained   bool    `json:"sustained"`
}

type Output struct {
	Label    string    `json:"label"`
	Started  time.Time `json:"started"`
	Vehicles int       `json:"vehicles"`
	StepSec  float64   `json:"step_s"`
	Workers  int       `json:"workers"`
	Linger   string    `json:"linger"`
	Steps    []Step    `json:"steps"`
}

func main() {
	var (
		brokers  = flag.String("brokers", "localhost:19092", "Kafka brokers")
		metrics  = flag.String("metrics", "http://localhost:9100/metrics", "fleetd metrics URL")
		ratesStr = flag.String("rates", "5000,10000,20000,40000,60000,80000,100000,150000", "offered pings/s per step")
		reps     = flag.Int("reps", 3, "repetitions per rate")
		stepDur  = flag.Duration("step", 30*time.Second, "duration of each step")
		vehicles = flag.Int("vehicles", 20000, "synthetic vehicles")
		workers  = flag.Int("workers", 4, "producer goroutines")
		linger   = flag.Duration("linger", 5*time.Millisecond, "producer linger")
		label    = flag.String("label", "baseline", "result name: bench/e1/results/<label>.json")
		outDir   = flag.String("out", "../bench/e1/results", "output directory")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var rates []int
	for _, s := range strings.Split(*ratesStr, ",") {
		r, err := strconv.Atoi(strings.TrimSpace(s))
		must(log, err)
		rates = append(rates, r)
	}
	must(log, bus.EnsureTopics(ctx, strings.Split(*brokers, ","), model.TopicPings))
	prod, err := bus.NewProducer(strings.Split(*brokers, ","), *linger)
	must(log, err)
	defer prod.Close()

	g := newGen(*vehicles, *workers)
	// Count only records the broker accepted: a failed produce (e.g. its context ended
	// at the step boundary) never reaches fleetd and must not count as lag.
	prod.OnError = func(error) { g.sent.Add(-1); g.failed.Add(1) }
	out := Output{Label: *label, Started: time.Now().UTC(), Vehicles: *vehicles,
		StepSec: stepDur.Seconds(), Workers: *workers, Linger: linger.String()}

	// Warm-up: put every vehicle in Redis and let connections settle. Not recorded.
	// fleetd's consumer starts at the latest offset and joins its group a few seconds
	// after start, so some warm-up pings may never be read. Once its count stops
	// moving, define base so that "processed - base == sent" from here on.
	log.Info("warm-up")
	g.run(ctx, prod, 5000, 10*time.Second)
	_ = prod.Flush(ctx)
	stable := waitStable(ctx, *metrics)
	base := stable - float64(g.sent.Load())
	log.Info("warm-up done", "sent", g.sent.Load(), "unread_at_start", int64(-base))

	for rep := 0; rep < *reps; rep++ {
		for _, rate := range rates {
			if ctx.Err() != nil {
				return
			}
			before, err := scrape(*metrics)
			must(log, err)
			sent0 := g.sent.Load()
			t0 := time.Now()
			g.run(ctx, prod, rate, *stepDur)
			elapsed := time.Since(t0).Seconds()
			after, err := scrape(*metrics)
			must(log, err)
			sent := g.sent.Load() - sent0

			d := after.sub(before)
			st := Step{
				Rate: rate, Rep: rep,
				Produced:    float64(sent) / elapsed,
				Applied:     d.processed / elapsed,
				P50ms:       finite(d.quantile(0.50) * 1000),
				P99ms:       finite(d.quantile(0.99) * 1000),
				MaxBucketMs: finite(d.maxBucket() * 1000),
				LagEnd:      g.sent.Load() - int64(after.processed-base),
			}
			if d.batchCount > 0 {
				st.MeanBatch = d.batchSum / d.batchCount
			}
			if d.processed > 0 {
				st.Stale = d.stale / d.processed
			}
			_ = prod.Flush(ctx)
			st.DrainSec = waitDrain(ctx, *metrics, base, g.sent.Load(), 180*time.Second)
			// Sustained = fleetd kept up during the step (no backlog worth > 1 s of input)
			// and the sender really offered the rate.
			st.Sustained = st.Produced >= 0.97*float64(rate) && st.LagEnd >= 0 && st.LagEnd < int64(rate)
			out.Steps = append(out.Steps, st)
			log.Info("step", "rate", rate, "rep", rep, "produced", int(st.Produced), "applied", int(st.Applied),
				"p50_ms", round(st.P50ms), "p99_ms", round(st.P99ms), "lag_end", st.LagEnd,
				"drain_s", round(st.DrainSec), "batch", int(st.MeanBatch), "sustained", st.Sustained)
			save(log, *outDir, out)
		}
	}
	log.Info("done", "failed_produces", g.failed.Load(), "file", filepath.Join(*outDir, *label+".json"))
}

// ---- synthetic fleet ----

type vehicle struct {
	id       string
	lat, lon float64
}

type gen struct {
	shards [][]vehicle
	sent   atomic.Int64
	failed atomic.Int64
}

func newGen(n, workers int) *gen {
	g := &gen{shards: make([][]vehicle, workers)}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < n; i++ {
		// Manhattan + inner Brooklyn/Queens box: dense, like the real fleet.
		v := vehicle{id: fmt.Sprintf("load-%05d", i), lat: 40.68 + r.Float64()*0.12, lon: -74.02 + r.Float64()*0.12}
		g.shards[i%workers] = append(g.shards[i%workers], v)
	}
	return g
}

// run offers `rate` pings/s for d, split across workers, paced in 10 ms ticks.
func (g *gen) run(ctx context.Context, prod *bus.Producer, rate int, d time.Duration) {
	const tick = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	var wg sync.WaitGroup
	for w := range g.shards {
		wg.Add(1)
		go func(vs []vehicle, seed uint64) {
			defer wg.Done()
			r := rand.New(rand.NewPCG(seed, 9))
			perSec := float64(rate) / float64(len(g.shards))
			start := time.Now()
			var due float64
			sent, idx := 0, 0
			t := time.NewTicker(tick)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
				// Catch up to the schedule: open loop, independent of the consumer.
				due = perSec * time.Since(start).Seconds()
				for ; sent < int(due); sent++ {
					if ctx.Err() != nil {
						return
					}
					v := &vs[idx]
					idx = (idx + 1) % len(vs)
					v.lat += (r.Float64() - 0.5) * 0.0004 // ~20 m jitter per ping
					v.lon += (r.Float64() - 0.5) * 0.0004
					p := model.Ping{VehicleID: v.id, Kind: model.KindHuman, Lat: v.lat, Lon: v.lon,
						Status: model.StatusIdle, TS: time.Now().UnixMilli()}
					_ = prod.Publish(ctx, model.TopicPings, geo.Cell(v.lat, v.lon, geo.PartitionRes), p)
					g.sent.Add(1)
				}
			}
		}(g.shards[w], uint64(w+1))
	}
	wg.Wait()
}

// ---- fleetd metrics ----

type snap struct {
	processed, stale     float64
	buckets              map[float64]float64 // le -> cumulative count
	batchSum, batchCount float64
}

func scrape(url string) (snap, error) {
	s := snap{buckets: map[float64]float64{}}
	resp, err := http.Get(url)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		sp := strings.LastIndexByte(line, ' ')
		if sp < 0 || strings.HasPrefix(line, "#") {
			continue
		}
		v, err := strconv.ParseFloat(line[sp+1:], 64)
		if err != nil {
			continue
		}
		name := line[:sp]
		switch {
		case strings.HasPrefix(name, "fleetline_pings_total{"):
			s.processed += v
			if strings.Contains(name, `"stale"`) {
				s.stale += v
			}
		case strings.HasPrefix(name, "fleetline_ingest_latency_seconds_bucket{le="):
			le := strings.TrimSuffix(strings.TrimPrefix(name, `fleetline_ingest_latency_seconds_bucket{le="`), `"}`)
			b := math.Inf(1)
			if le != "+Inf" {
				b, _ = strconv.ParseFloat(le, 64)
			}
			s.buckets[b] = v
		case name == "fleetline_ping_batch_size_sum":
			s.batchSum = v
		case name == "fleetline_ping_batch_size_count":
			s.batchCount = v
		}
	}
	return s, sc.Err()
}

func (a snap) sub(b snap) snap {
	d := snap{processed: a.processed - b.processed, stale: a.stale - b.stale,
		batchSum: a.batchSum - b.batchSum, batchCount: a.batchCount - b.batchCount,
		buckets: map[float64]float64{}}
	for k, v := range a.buckets {
		d.buckets[k] = v - b.buckets[k]
	}
	return d
}

// quantile interpolates linearly inside the histogram bucket, like PromQL's
// histogram_quantile. Returns +Inf if the quantile is beyond the last finite bucket.
func (s snap) quantile(q float64) float64 {
	les := make([]float64, 0, len(s.buckets))
	for le := range s.buckets {
		les = append(les, le)
	}
	sort.Float64s(les)
	if len(les) == 0 || s.buckets[math.Inf(1)] == 0 {
		return math.NaN()
	}
	target := q * s.buckets[math.Inf(1)]
	prevLe, prevC := 0.0, 0.0
	for _, le := range les {
		c := s.buckets[le]
		if c >= target {
			if math.IsInf(le, 1) {
				return math.Inf(1)
			}
			if c == prevC {
				return le
			}
			return prevLe + (le-prevLe)*(target-prevC)/(c-prevC)
		}
		prevLe, prevC = le, c
	}
	return math.Inf(1)
}

// maxBucket is the upper bound of the highest non-empty bucket: a bound on the worst batch.
func (s snap) maxBucket() float64 {
	les := make([]float64, 0, len(s.buckets))
	for le := range s.buckets {
		les = append(les, le)
	}
	sort.Float64s(les)
	prev, best := 0.0, 0.0
	for _, le := range les {
		if s.buckets[le] > prev {
			best = le
		}
		prev = s.buckets[le]
	}
	return best
}

// waitDrain polls until fleetd has processed everything we sent (base = fleetd's
// counter before we started; fleetd only receives our pings during a load test).
// Returns -1 if it doesn't catch up within limit.
func waitDrain(ctx context.Context, url string, base float64, sent int64, limit time.Duration) float64 {
	t0 := time.Now()
	for time.Since(t0) < limit && ctx.Err() == nil {
		s, err := scrape(url)
		if err == nil && int64(s.processed-base) >= sent {
			return time.Since(t0).Seconds()
		}
		time.Sleep(200 * time.Millisecond)
	}
	return -1
}

// waitStable returns fleetd's processed count once it stops changing.
func waitStable(ctx context.Context, url string) float64 {
	last := -1.0
	for ctx.Err() == nil {
		time.Sleep(time.Second)
		s, err := scrape(url)
		if err != nil {
			continue
		}
		if s.processed == last {
			return last
		}
		last = s.processed
	}
	return last
}

// finite maps +Inf/NaN (beyond the histogram / no data) to -1 so the JSON stays valid.
func finite(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return -1
	}
	return x
}

func save(log *slog.Logger, dir string, out Output) {
	must(log, os.MkdirAll(dir, 0o755))
	b, _ := json.MarshalIndent(out, "", "  ")
	must(log, os.WriteFile(filepath.Join(dir, out.Label+".json"), append(b, '\n'), 0o644))
}

func round(x float64) float64 { return math.Round(x*10) / 10 }

func must(log *slog.Logger, err error) {
	if err != nil {
		log.Error("loadgen failed", "err", err)
		os.Exit(1)
	}
}
