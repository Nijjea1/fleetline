// Command fleetd is the dispatch system: it ingests vehicle pings into Redis, queues ride
// requests, runs the dispatch policy on a fixed event-time cadence, publishes assignments,
// and exposes metrics. It never reads simulator memory; Kafka is its only input (D-006).
//
//	go run ./cmd/fleetd
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Nijjea1/fleetline/core/internal/bus"
	"github.com/Nijjea1/fleetline/core/internal/dispatch"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/metrics"
	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/state"
)

type server struct {
	log      *slog.Logger
	store    *state.Store
	prod     *bus.Producer
	policy   dispatch.Policy
	patience int64 // ms
	window   int64 // ms of event time between dispatch rounds
	summary  *metrics.Summary

	watermark atomic.Int64 // latest event time seen (D-007)

	mu        sync.Mutex
	pending   []model.RideRequest
	requestTS map[int64]int64 // trip -> request time, to compute waits from trip events
	kindOf    map[int64]string
}

func main() {
	var (
		brokers   = flag.String("brokers", "localhost:19092", "Kafka brokers")
		redisAddr = flag.String("redis", "localhost:6379", "Redis address")
		modelPath = flag.String("model", "../data/derived/travel_model_2026-09-16.json", "travel model json")
		httpAddr  = flag.String("http", ":9100", "metrics/stats HTTP address")
		radius    = flag.Float64("radius", 3, "candidate search radius, miles")
		k         = flag.Int("k", 8, "candidates per vehicle kind")
		patience  = flag.Duration("patience", 10*time.Minute, "drop requests older than this")
		window    = flag.Duration("window", time.Second, "event time between dispatch rounds")
		reset     = flag.Bool("reset", true, "clear Redis state on start")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, log, strings.Split(*brokers, ","), *redisAddr, *modelPath, *httpAddr,
		*radius, *k, *patience, *window, *reset); err != nil {
		log.Error("fleetd failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, brokers []string, redisAddr, modelPath, httpAddr string,
	radius float64, k int, patience, window time.Duration, reset bool) error {

	travel, err := geo.LoadTravelModel(modelPath)
	if err != nil {
		return err
	}
	store := state.New(redisAddr, 0)
	defer store.Close()
	if err := store.Ping(ctx); err != nil {
		return err
	}
	if reset {
		if err := store.Reset(ctx); err != nil {
			return err
		}
	}
	if err := bus.EnsureTopics(ctx, brokers, model.TopicPings, model.TopicRequests,
		model.TopicAssignments, model.TopicTripEvents); err != nil {
		return err
	}
	prod, err := bus.NewProducer(brokers, time.Millisecond)
	if err != nil {
		return err
	}
	defer prod.Close()

	s := &server{
		log: log, store: store, prod: prod,
		policy:   &dispatch.Greedy{Travel: travel, RadiusMi: radius, K: k},
		patience: patience.Milliseconds(), window: window.Milliseconds(),
		summary: metrics.NewSummary(), requestTS: map[int64]int64{}, kindOf: map[int64]string{},
	}

	cons, err := bus.NewConsumer(brokers, "fleetd", false,
		model.TopicPings, model.TopicRequests, model.TopicTripEvents)
	if err != nil {
		return err
	}
	defer cons.Close()

	go s.serveHTTP(ctx, httpAddr)
	go s.dispatchLoop(ctx)
	log.Info("fleetd ready", "policy", s.policy.Name(), "window", window, "http", httpAddr)
	return cons.RunBatch(ctx, s.handleBatch)
}

func (s *server) advanceWatermark(ts int64) {
	for {
		cur := s.watermark.Load()
		if ts <= cur || s.watermark.CompareAndSwap(cur, ts) {
			return
		}
	}
}

// handleBatch routes one Kafka poll: pings go to Redis in a single pipeline.
func (s *server) handleBatch(msgs []bus.Message) {
	pings := make([]state.Ping, 0, len(msgs))
	var oldest time.Time
	for _, m := range msgs {
		switch m.Topic {
		case model.TopicPings:
			var p model.Ping
			if json.Unmarshal(m.Value, &p) != nil {
				metrics.PingsTotal.WithLabelValues("bad").Inc()
				continue
			}
			pings = append(pings, p)
			if oldest.IsZero() || m.BrokerTime.Before(oldest) {
				oldest = m.BrokerTime
			}
		case model.TopicRequests:
			var r model.RideRequest
			if json.Unmarshal(m.Value, &r) != nil {
				continue
			}
			s.advanceWatermark(r.TS)
			metrics.RequestsTotal.Inc()
			s.mu.Lock()
			s.pending = append(s.pending, r)
			s.requestTS[r.TripID] = r.TS
			s.mu.Unlock()
		case model.TopicTripEvents:
			var e model.TripEvent
			if json.Unmarshal(m.Value, &e) == nil {
				s.onTripEvent(e)
			}
		}
	}
	if len(pings) == 0 {
		return
	}
	res, err := s.store.ApplyPings(context.Background(), pings)
	if err != nil {
		s.log.Error("apply pings", "err", err)
		metrics.PingsTotal.WithLabelValues("error").Add(float64(len(pings)))
		return
	}
	now := time.Now()
	for i, r := range res {
		metrics.PingsTotal.WithLabelValues([...]string{"stale", "applied", "position_only"}[r]).Inc()
		s.advanceWatermark(pings[i].TS)
	}
	metrics.PingBatchSize.Observe(float64(len(pings)))
	// Every ping in the batch waited at least this long; the oldest bounds the worst case.
	metrics.IngestLatency.Observe(now.Sub(oldest).Seconds())
}

func (s *server) onTripEvent(e model.TripEvent) {
	switch e.Type {
	case model.TripPickup:
		s.mu.Lock()
		reqTS, ok := s.requestTS[e.TripID]
		delete(s.requestTS, e.TripID)
		s.mu.Unlock()
		if ok {
			wait := float64(e.TS-reqTS) / 1000
			metrics.RiderWait.Observe(wait)
			s.summary.Pickup(wait)
		}
		metrics.TripsTotal.WithLabelValues("served").Inc()
	case model.TripExpired:
		s.mu.Lock()
		delete(s.requestTS, e.TripID)
		s.mu.Unlock()
		s.summary.Expired()
		metrics.TripsTotal.WithLabelValues("expired").Inc()
	}
}

// dispatchLoop runs one dispatch round each time event time advances by `window`.
// Tying rounds to event time (not wall time) keeps decisions identical at any replay speed.
func (s *server) dispatchLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Millisecond)
	defer t.Stop()
	var last int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := s.watermark.Load()
		if now == 0 || now-last < s.window {
			continue
		}
		last = now
		s.round(ctx, now)
	}
}

func (s *server) round(ctx context.Context, now int64) {
	start := time.Now()
	s.mu.Lock()
	batch := s.pending
	s.pending = nil
	s.mu.Unlock()

	// Drop requests whose rider has given up (the world reports the expiry separately).
	live := batch[:0]
	for _, r := range batch {
		if now-r.TS < s.patience {
			live = append(live, r)
		}
	}

	assigned, err := s.policy.Dispatch(ctx, now, live, s.store)
	if err != nil && !errors.Is(err, context.Canceled) {
		s.log.Error("dispatch", "err", err)
	}
	done := make(map[int64]bool, len(assigned))
	for _, a := range assigned {
		done[a.TripID] = true
		_ = s.prod.Publish(ctx, model.TopicAssignments, a.VehicleID, a)
		kind := string(a.Kind)
		metrics.AssignmentsTotal.WithLabelValues(kind).Inc()
		s.summary.Assigned(kind)
	}

	// Unmatched requests go back to the front of the queue (they are the oldest).
	var left []model.RideRequest
	for _, r := range live {
		if !done[r.TripID] {
			left = append(left, r)
		}
	}
	s.mu.Lock()
	s.pending = append(left, s.pending...)
	metrics.Pending.Set(float64(len(s.pending)))
	s.mu.Unlock()
	metrics.Watermark.Set(float64(now))
	metrics.DispatchRound.Observe(time.Since(start).Seconds())
}

func (s *server) serveHTTP(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Policy    string `json:"policy"`
			Watermark string `json:"sim_time"`
			metrics.Snapshot
		}{s.policy.Name(), time.UnixMilli(s.watermark.Load()).UTC().Format(time.DateTime), s.summary.Snapshot()})
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.log.Error("http", "err", err)
	}
}
