// Command calibrate fits hourly driver supply so that the greedy baseline reproduces the
// real Uber/Lyft median wait in every hour.
//
// Why: supply (how many drivers are online) is not in the public data. We only know how
// many cars were *busy*. Online = busy / utilization, and utilization is unknown, so we
// infer it: run the day, compare simulated and real median waits per hour, nudge that
// hour's utilization, repeat. This is standard simulation calibration: tune the
// unobserved inputs until the baseline matches reality, then change one thing at a time.
//
//	go run ./cmd/calibrate -max-pickup 600s -iters 8 -out ../bench/calibration/sep16.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/dispatch"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/offline"
	"github.com/Nijjea1/fleetline/core/internal/replay"
)

// Iteration records one calibration pass.
type Iteration struct {
	Utilization [24]float64 `json:"utilization"`
	SimP50      [24]float64 `json:"sim_p50_s"`
	RealP50     [24]float64 `json:"real_p50_s"`
	RelErr      [24]float64 `json:"rel_err"` // (sim - real) / real
	MeanAbsErr  float64     `json:"mean_abs_rel_err"`
	MaxAbsErr   float64     `json:"max_abs_rel_err"`
	Unserved    float64     `json:"unserved_rate"`
	StepScale   float64     `json:"step_scale"`
	Backtracked bool        `json:"backtracked,omitempty"`
	RuntimeSec  float64     `json:"runtime_s"`
}

// Output is the calibration file.
type Output struct {
	Replay      string         `json:"replay"`
	MaxPickup   string         `json:"max_pickup"`
	Seed        uint64         `json:"seed"`
	Offers      offline.Offers `json:"offers"`
	DeclineRate float64        `json:"decline_rate_best,omitempty"`
	Damping     float64        `json:"damping"`
	MaxStep     float64        `json:"max_step"`
	Iterations  []Iteration    `json:"iterations"`
	Utilization [24]float64    `json:"utilization_by_hour"` // best iteration
	BestIter    int            `json:"best_iteration"`
}

func main() {
	var (
		replayPath = flag.String("replay", "../data/derived/day_2026-09-16_seed7.parquet", "replay day")
		modelPath  = flag.String("model", "../data/derived/travel_model_2026-09-16.json", "travel model")
		maxPickup  = flag.Duration("max-pickup", 600*time.Second, "greedy pickup-ETA cap")
		iters      = flag.Int("iters", 8, "calibration iterations")
		start      = flag.Float64("start", 0.75, "initial utilization for every hour")
		damping    = flag.Float64("damping", 0.3, "exponent on the wait ratio per step (1 = full step)")
		maxStep    = flag.Float64("max-step", 0.08, "max relative change of an hour's utilization per iteration")
		seed       = flag.Uint64("seed", 7, "random seed")
		offerDelay = flag.Duration("offer-delay", 0, "driver answer delay (offer model, D-017)")
		declBase   = flag.Float64("decline-base", 0, "decline probability at a 0-minute pickup")
		declPerMin = flag.Float64("decline-per-min", 0, "added decline probability per pickup minute")
		out        = flag.String("out", "../bench/calibration/sep16.json", "output file")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	travel, err := geo.LoadTravelModel(*modelPath)
	must(log, err)
	trips, err := replay.Load(*replayPath)
	must(log, err)
	day := time.UnixMilli(trips[0].TS).UTC().Truncate(24 * time.Hour)

	var u [24]float64
	for h := range u {
		u[h] = *start
	}
	offers := offline.Offers{Delay: *offerDelay, DeclineBase: *declBase, DeclinePerMin: *declPerMin}
	res := Output{Replay: filepath.Base(*replayPath), MaxPickup: maxPickup.String(), Seed: *seed,
		Offers: offers, Damping: *damping, MaxStep: *maxStep}
	best := math.Inf(1)
	var bestU [24]float64
	scale := 1.0 // shrinks when an iteration gets clearly worse (backtracking)
	save := func() {
		must(log, os.MkdirAll(filepath.Dir(*out), 0o755))
		b, _ := json.MarshalIndent(res, "", "  ")
		must(log, os.WriteFile(*out, append(b, '\n'), 0o644))
	}
	for it := 0; it < *iters; it++ {
		uu := u
		cfg := offline.Config{
			Travel: travel,
			Policy: &dispatch.Greedy{Travel: travel, RadiusMi: 3, K: 8, MaxPickupSec: maxPickup.Seconds()},
			From:   day.Add(time.Hour).UnixMilli(), To: day.Add(24 * time.Hour).UnixMilli(),
			Warmup: time.Hour, Drain: 30 * time.Minute, Patience: 10 * time.Minute, Window: time.Second,
			Supply:  offline.DataSupply{Busy: travel.BusyVehiclesByHour, PerHour: &uu},
			AVShare: 0.1, Seed: *seed, Offers: offers,
		}
		r, err := offline.Run(ctx, cfg, trips)
		must(log, err)
		sum := offline.Summarize(r)

		itr := Iteration{Utilization: u, Unserved: sum.UnservedRate, RuntimeSec: r.Elapsed.Seconds(), StepScale: scale}
		n := 0.0
		next := u
		worst := 0
		for _, hs := range sum.ByHour {
			h := hs.Hour
			itr.SimP50[h], itr.RealP50[h] = hs.Sim.P50, hs.Real.P50
			if hs.Real.P50 <= 0 || hs.Sim.P50 <= 0 {
				continue
			}
			e := (hs.Sim.P50 - hs.Real.P50) / hs.Real.P50
			itr.RelErr[h] = e
			itr.MeanAbsErr += math.Abs(e)
			if math.Abs(e) > itr.MaxAbsErr {
				itr.MaxAbsErr, worst = math.Abs(e), h
			}
			n++
			// Sim too slow (ratio < 1) -> lower utilization -> more drivers online.
			// Bounded step: near the wild-goose-chase regime waits react very nonlinearly
			// to supply, and unbounded steps made the first calibration oscillate
			// (mean error 0.755 -> 0.564 -> 0.989).
			ratio := hs.Real.P50 / hs.Sim.P50
			ms := *maxStep * scale
			step := clamp(math.Pow(ratio, *damping), 1-ms, 1+ms)
			next[h] = clamp(u[h]*step, 0.3, 0.97)
		}
		itr.MeanAbsErr /= n
		// Hours are coupled (drivers and backlog carry into the next hour), so a step
		// that helps each hour alone can push the day into collapse. If this iteration
		// is clearly worse than the best, go back to the best settings with half the step.
		if itr.MeanAbsErr > best*1.10 {
			itr.Backtracked = true
			scale /= 2
			u = bestU
		} else {
			if itr.MeanAbsErr < best {
				best, bestU, res.Utilization, res.BestIter = itr.MeanAbsErr, itr.Utilization, itr.Utilization, it
				res.DeclineRate = sum.DeclineRate
			}
			u = next
		}
		u[0] = u[1] // hour 0 is warm-up only; follow hour 1
		res.Iterations = append(res.Iterations, itr)
		save()
		log.Info("iteration", "i", it, "mean_abs_rel_err", fmt.Sprintf("%.3f", itr.MeanAbsErr),
			"max_abs_rel_err", fmt.Sprintf("%.3f", itr.MaxAbsErr), "worst_hour", worst,
			"backtracked", itr.Backtracked, "step_scale", scale,
			"unserved", fmt.Sprintf("%.4f", itr.Unserved), "runtime", r.Elapsed.Round(time.Second))
	}
	save()
	log.Info("wrote", "file", *out, "best_iteration", res.BestIter, "mean_abs_rel_err", fmt.Sprintf("%.3f", best))
}

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

func must(log *slog.Logger, err error) {
	if err != nil {
		log.Error("calibrate failed", "err", err)
		os.Exit(1)
	}
}
