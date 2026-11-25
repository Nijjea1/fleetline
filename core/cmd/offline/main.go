// Command offline runs one deterministic dispatch scenario and writes results.
//
//	go run ./cmd/offline -from 00:00 -to 23:59 -out ../bench/runs/fullday
//
// Output directory:
//
//	summary.json   config + headline stats + per-hour real-vs-simulated waits
//	trips.csv      one row per request (outcome, simulated wait, real wait)
//	samples.csv    fleet state every simulated minute
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/dispatch"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/offline"
	"github.com/Nijjea1/fleetline/core/internal/replay"
)

// Params are the knobs of one run, saved verbatim in summary.json so any result can be
// reproduced from its own output file.
type Params struct {
	Replay      string  `json:"replay"`
	Model       string  `json:"model"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	Warmup      string  `json:"warmup"`
	Patience    string  `json:"patience"`
	Window      string  `json:"window"`
	Policy      string  `json:"policy"`
	RadiusMi    float64 `json:"radius_mi"`
	K           int     `json:"k"`
	AgeWeight   float64 `json:"age_weight"`
	MaxPickup   string  `json:"max_pickup"`
	NoSkip      bool    `json:"no_skip,omitempty"`
	Supply      string  `json:"supply"`
	Fleet       int     `json:"fleet,omitempty"`
	Utilization float64 `json:"utilization,omitempty"`
	SupplyFile  string  `json:"supply_file,omitempty"`
	AVShare     float64 `json:"av_share"`
	Seed        uint64  `json:"seed"`
}

func main() {
	var p Params
	flag.StringVar(&p.Replay, "replay", "../data/derived/day_2026-09-16_seed7.parquet", "replay day parquet")
	flag.StringVar(&p.Model, "model", "../data/derived/travel_model_2026-09-16.json", "travel model json")
	flag.StringVar(&p.From, "from", "07:00", "measured window start HH:MM")
	flag.StringVar(&p.To, "to", "10:00", "measured window end HH:MM (23:59 = end of day)")
	flag.StringVar(&p.Warmup, "warmup", "45m", "simulated before -from, excluded from stats")
	flag.StringVar(&p.Patience, "patience", "10m", "riders give up after this")
	flag.StringVar(&p.Window, "window", "1s", "event time between dispatch rounds")
	flag.StringVar(&p.Policy, "policy", "greedy", "dispatch policy: greedy or batched")
	flag.Float64Var(&p.RadiusMi, "radius", 3, "candidate search radius, miles")
	flag.IntVar(&p.K, "k", 8, "candidates per vehicle kind")
	flag.Float64Var(&p.AgeWeight, "age-weight", 3, "batched: seconds of pickup ETA one second of waiting is worth")
	flag.BoolVar(&p.NoSkip, "no-skip", false, "disable the skip-unchanged dispatch optimization")
	flag.StringVar(&p.MaxPickup, "max-pickup", "0s", "reject matches with a longer pickup ETA (0s = no cap)")
	flag.StringVar(&p.Supply, "supply", "data", "data (follow real busy curve) or fixed")
	flag.IntVar(&p.Fleet, "fleet", 20000, "with -supply fixed: number of vehicles")
	flag.Float64Var(&p.Utilization, "utilization", 0.75, "with -supply data: busy / online")
	flag.StringVar(&p.SupplyFile, "supply-file", "", "with -supply data: per-hour utilization from cmd/calibrate")
	flag.Float64Var(&p.AVShare, "av-share", 0.1, "fraction of new drivers that are robotaxis")
	flag.Uint64Var(&p.Seed, "seed", 7, "random seed (fleet placement, log-offs)")
	out := flag.String("out", "", "output directory (required)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *out == "" {
		log.Error("-out is required")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, log, p, *out); err != nil {
		log.Error("offline run failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, p Params, out string) error {
	travel, err := geo.LoadTravelModel(p.Model)
	if err != nil {
		return err
	}
	trips, err := replay.Load(p.Replay)
	if err != nil {
		return err
	}
	day := time.UnixMilli(trips[0].TS).UTC().Truncate(24 * time.Hour)
	from, err := clockOn(day, p.From)
	if err != nil {
		return err
	}
	to, err := clockOn(day, p.To)
	if err != nil {
		return err
	}
	if p.To == "23:59" {
		to = day.Add(24 * time.Hour).UnixMilli()
	}
	durs := map[string]time.Duration{}
	for name, s := range map[string]string{"warmup": p.Warmup, "patience": p.Patience, "window": p.Window, "max-pickup": p.MaxPickup} {
		d, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("-%s: %w", name, err)
		}
		durs[name] = d
	}

	var supply offline.Supply
	switch p.Supply {
	case "data":
		ds := offline.DataSupply{Busy: travel.BusyVehiclesByHour, Utilization: p.Utilization}
		if p.SupplyFile != "" {
			var cal struct {
				U [24]float64 `json:"utilization_by_hour"`
			}
			b, err := os.ReadFile(p.SupplyFile)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(b, &cal); err != nil {
				return fmt.Errorf("%s: %w", p.SupplyFile, err)
			}
			ds.PerHour = &cal.U
			p.Utilization = 0
		}
		supply = ds
		p.Fleet = 0
	case "fixed":
		supply = offline.FixedSupply(p.Fleet)
		p.Utilization = 0
	default:
		return fmt.Errorf("unknown -supply %q", p.Supply)
	}
	var policy dispatch.Policy
	switch p.Policy {
	case "greedy":
		policy = &dispatch.Greedy{Travel: travel, RadiusMi: p.RadiusMi, K: p.K,
			MaxPickupSec: durs["max-pickup"].Seconds(), NoSkip: p.NoSkip}
	case "batched":
		policy = &dispatch.Batched{Travel: travel, RadiusMi: p.RadiusMi, K: p.K, MaxPickupSec: durs["max-pickup"].Seconds(), AgeWeight: p.AgeWeight}
	default:
		return fmt.Errorf("unknown -policy %q", p.Policy)
	}

	cfg := offline.Config{
		Travel: travel, Policy: policy, From: from, To: to,
		Warmup: durs["warmup"], Patience: durs["patience"], Window: durs["window"],
		Drain: 30 * time.Minute, Supply: supply, AVShare: p.AVShare, Seed: p.Seed,
	}
	log.Info("running", "from", p.From, "to", p.To, "policy", p.Policy, "supply", p.Supply,
		"utilization", p.Utilization, "seed", p.Seed)
	res, err := offline.Run(ctx, cfg, trips)
	if err != nil {
		return err
	}
	sum := offline.Summarize(res)
	log.Info("done", "requests", sum.Requests, "unserved_rate", fmt.Sprintf("%.4f", sum.UnservedRate),
		"sim_p50", fmt.Sprintf("%.0f", sum.Sim.P50), "real_p50", fmt.Sprintf("%.0f", sum.Real.P50),
		"sim_p90", fmt.Sprintf("%.0f", sum.Sim.P90), "real_p90", fmt.Sprintf("%.0f", sum.Real.P90),
		"runtime", res.Elapsed.Round(time.Millisecond))
	return write(out, p, sum, res)
}

func write(dir string, p Params, sum offline.Summary, res *offline.Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(struct {
		Params  Params          `json:"params"`
		Summary offline.Summary `json:"summary"`
	}{p, sum}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}

	rows := [][]string{{"trip_id", "request_ts", "warmup", "outcome", "wait_s", "real_wait_s", "kind", "pickup_eta_s"}}
	for _, t := range res.Trips {
		rows = append(rows, []string{
			strconv.FormatInt(t.TripID, 10), strconv.FormatInt(t.RequestTS, 10),
			strconv.FormatBool(t.Warmup), string(t.Outcome),
			strconv.FormatFloat(t.WaitS, 'f', 3, 64), strconv.FormatInt(t.RealWaitS, 10), t.Kind,
			strconv.FormatFloat(t.PickupETAS, 'f', 0, 64),
		})
	}
	if err := writeCSV(filepath.Join(dir, "trips.csv"), rows); err != nil {
		return err
	}
	rows = [][]string{{"ts", "target", "online", "busy", "pending"}}
	for _, s := range res.Samples {
		rows = append(rows, []string{strconv.FormatInt(s.TS, 10), strconv.Itoa(s.Target),
			strconv.Itoa(s.Online), strconv.Itoa(s.Busy), strconv.Itoa(s.Pending)})
	}
	return writeCSV(filepath.Join(dir, "samples.csv"), rows)
}

func writeCSV(path string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func clockOn(day time.Time, hhmm string) (int64, error) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0, fmt.Errorf("bad time %q: %w", hhmm, err)
	}
	return day.Add(time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute).UnixMilli(), nil
}
