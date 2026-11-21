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
		fleet      = flag.Int("fleet", 8000, "number of vehicles")
		avShare    = flag.Float64("av-share", 0.1, "fraction of the fleet that are robotaxis")
		speed      = flag.Float64("speed", 10, "simulated seconds per wall second")
		seed       = flag.Uint64("seed", 7, "fleet placement seed")
		pingEvery  = flag.Duration("ping", 4*time.Second, "vehicle ping interval (simulated)")
		patience   = flag.Duration("patience", 10*time.Minute, "riders give up after this")
		linger     = flag.Duration("linger", 5*time.Millisecond, "Kafka producer linger")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, log, *replayPath, *modelPath, strings.Split(*brokers, ","), *from, *to,
		*fleet, *avShare, *speed, *seed, *pingEvery, *patience, *linger); err != nil {
		log.Error("worldsim failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, replayPath, modelPath string, brokers []string,
	from, to string, fleet int, avShare, speed float64, seed uint64,
	pingEvery, patience, linger time.Duration) error {

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
	window := trips[:0:0]
	for _, t := range trips {
		if t.TS >= start && t.TS < end {
			window = append(window, t)
		}
	}
	log.Info("replay window", "from", from, "to", to, "requests", len(window), "fleet", fleet, "speed", speed)

	// Place the fleet where demand is in the first 30 minutes of the window.
	var early []model.RideRequest
	for _, t := range window {
		if t.TS > start+30*60_000 {
			break
		}
		early = append(early, t.RideRequest)
	}
	w := world.New(world.Config{Travel: travel, PingInterval: pingEvery, Patience: patience},
		start, world.SpawnFleet(fleet, avShare, early, seed))

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
		drainUntil                     = end + patience.Milliseconds() // let last riders resolve
		wallStart                      = time.Now()
		lastLog                        = time.Now()
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
		simNow := start + int64(float64(time.Since(wallStart).Milliseconds())*speed)
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
