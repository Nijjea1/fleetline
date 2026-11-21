// Command worldsim plays the city: it replays real NYC ride requests in (accelerated)
// time, drives a simulated fleet, and talks to the dispatch system only through Kafka.
//
//	go run ./cmd/worldsim -from 07:00 -to 10:00 -fleet 8000 -speed 10
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/bus"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/replay"
	"github.com/Nijjea1/fleetline/core/internal/world"
)

func main() {
	var (
		replayPath = flag.String("replay", "../data/derived/day_2026-09-16_seed7.parquet", "replay day parquet")
		modelPath  = flag.String("model", "../data/derived/travel_model_2026-09-16.json", "travel model json")
		brokers    = flag.String("brokers", "localhost:19092", "Kafka brokers, comma separated")
		from       = flag.String("from", "07:00", "replay window start (HH:MM, NYC time)")
		to         = flag.String("to", "10:00", "replay window end (HH:MM)")
		fleet      = flag.Int("fleet", 0, "number of vehicles (0 = size from data, see -utilization)")
		util       = flag.Float64("utilization", 0.75, "with -fleet 0: fleet = real busy vehicles / utilization")
		avShare    = flag.Float64("av-share", 0.1, "fraction of the fleet that are robotaxis")
		speed      = flag.Float64("speed", 10, "simulated seconds per wall second")
		seed       = flag.Uint64("seed", 7, "fleet placement seed")
		pingEvery  = flag.Duration("ping", 4*time.Second, "vehicle ping interval (simulated)")
		patience   = flag.Duration("patience", 10*time.Minute, "riders give up after this")
		warmup     = flag.Duration("warmup", 45*time.Minute, "simulate this long before -from; excluded from stats")
		linger     = flag.Duration("linger", 5*time.Millisecond, "Kafka producer linger")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, log, *replayPath, *modelPath, strings.Split(*brokers, ","), *from, *to,
		*fleet, *util, *avShare, *speed, *seed, *pingEvery, *patience, *warmup, *linger); err != nil {
		log.Error("worldsim failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, replayPath, modelPath string, brokers []string,
	from, to string, fleet int, util, avShare, speed float64, seed uint64,
	pingEvery, patience, warmup, linger time.Duration) error {

	travel, err := geo.LoadTravelModel(modelPath)
	if err != nil {
		return err
	}
	trips, err := replay.Load(replayPath)
	if err != nil {
		return err
	}
	day := time.UnixMilli(trips[0].TS).UTC().Truncate(24 * time.Hour)
	start, err := clockOn(day, from)
	if err != nil {
		return err
	}
	end, err := clockOn(day, to)
	if err != nil {
		return err
	}
	if fleet == 0 {
		// Size supply from data: the real number of busy cars at the window's start hour,
		// plus idle headroom. Utilization is an explicit, documented assumption.
		busy := travel.BusyVehiclesByHour[time.UnixMilli(start).UTC().Hour()]
		if busy == 0 {
			return fmt.Errorf("travel model has no busy-vehicle data; rerun fleetline-sim calibrate")
		}
		fleet = int(float64(busy) / util)
		log.Info("fleet sized from data", "real_busy", busy, "utilization", util, "fleet", fleet)
	}
	// Warm-up: the simulation starts empty (every car idle), which is nothing like a real
	// 7 am. We run `warmup` first so cars get busy and spread out, and exclude those
	// requests from the stats. Standard practice in discrete-event simulation.
	simStart := start - warmup.Milliseconds()
	window := trips[:0:0]
	for _, t := range trips {
		if t.TS >= simStart && t.TS < end {
			t.Warmup = t.TS < start
			window = append(window, t)
		}
	}
	log.Info("replay window", "from", from, "to", to, "warmup", warmup,
		"requests", len(window), "fleet", fleet, "speed", speed)

	// Start idle cars where the previous hour's trips ended: that is where idle supply
	// really is. (Using pickup points of the requests we're about to replay would park
	// cars exactly where future riders appear: a data leak.)
	var spawnAt []world.Point
	for _, t := range trips {
		if t.TS >= simStart-60*60_000 && t.TS < simStart {
			spawnAt = append(spawnAt, world.Point{Lat: t.DropLat, Lon: t.DropLon})
		}
	}
	if len(spawnAt) == 0 {
		return fmt.Errorf("no trips in the hour before %s to place the fleet", time.UnixMilli(simStart).UTC())
	}
	w := world.New(world.Config{Travel: travel, PingInterval: pingEvery, Patience: patience},
		simStart, world.SpawnFleet(fleet, avShare, spawnAt, seed))

	if err := bus.EnsureTopics(ctx, brokers, model.TopicPings, model.TopicRequests,
		model.TopicAssignments, model.TopicTripEvents); err != nil {
		return fmt.Errorf("ensure topics: %w", err)
	}
	prod, err := bus.NewProducer(brokers, linger)
	if err != nil {
		return err
	}
	var produceErrs atomic.Int64
	prod.OnError = func(error) { produceErrs.Add(1) }
	defer prod.Close()

	// Assignments arrive on a separate goroutine; the world itself stays single-threaded.
	assignments := make(chan model.Assignment, 10_000)
	cons, err := bus.NewConsumer(brokers, fmt.Sprintf("worldsim-%d", time.Now().UnixNano()), false, model.TopicAssignments)
	if err != nil {
		return err
	}
	defer cons.Close()
	go func() {
		_ = cons.Run(ctx, func(m bus.Message) {
			var a model.Assignment
			if json.Unmarshal(m.Value, &a) == nil {
				assignments <- a
			}
		})
	}()

	var (
		next                           int // next request to publish
		nPings, nEvents, nAssign, nBad int64
		pickups, dropoffs, expired     int64
		tick                           = 100 * time.Millisecond
		// Keep running after the last request so every assigned rider is picked up and
		// counted; otherwise the slowest pickups silently vanish from the stats.
		drainUntil = end + patience.Milliseconds() + 30*60_000
		wallStart  = time.Now()
		lastLog    = time.Now()
	)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("interrupted")
			return nil
		case <-ticker.C:
		}
		simNow := simStart + int64(float64(time.Since(wallStart).Milliseconds())*speed)
		if simNow > drainUntil {
			break
		}

	drain:
		for {
			select {
			case a := <-assignments:
				nAssign++
				if err := w.Assign(a); err != nil {
					nBad++
				}
			default:
				break drain
			}
		}

		for next < len(window) && window[next].TS <= simNow {
			r := window[next].RideRequest
			w.Request(r)
			_ = prod.Publish(ctx, model.TopicRequests, geo.Cell(r.PickLat, r.PickLon, geo.PartitionRes), r)
			next++
		}

		step := w.Advance(simNow)
		for _, p := range step.Pings {
			_ = prod.Publish(ctx, model.TopicPings, geo.Cell(p.Lat, p.Lon, geo.PartitionRes), p)
		}
		for _, e := range step.Events {
			_ = prod.Publish(ctx, model.TopicTripEvents, e.VehicleID, e)
			switch e.Type {
			case model.TripPickup:
				pickups++
			case model.TripDropoff:
				dropoffs++
			case model.TripExpired:
				expired++
			}
		}
		nPings += int64(len(step.Pings))
		nEvents += int64(len(step.Events))

		if time.Since(lastLog) > 5*time.Second {
			lastLog = time.Now()
			log.Info("progress",
				"sim", time.UnixMilli(simNow).UTC().Format("15:04:05"),
				"requests", next, "pickups", pickups, "expired", expired,
				"assignments", nAssign, "rejected", nBad, "pings", nPings,
				"produce_errors", produceErrs.Load())
		}
	}
	log.Info("done", "requests", next, "pickups", pickups, "dropoffs", dropoffs, "expired", expired,
		"assignments", nAssign, "rejected", nBad, "pings", nPings, "produce_errors", produceErrs.Load())
	return nil
}

// clockOn returns day + HH:MM as event-time milliseconds.
func clockOn(day time.Time, hhmm string) (int64, error) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0, fmt.Errorf("bad time %q: %w", hhmm, err)
	}
	return day.Add(time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute).UnixMilli(), nil
}
