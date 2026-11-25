// Command export-replay runs the offline simulator over a short window and writes the
// compact files the dashboard's replay mode plays back (web/public/replay/):
//
//	meta.json    window, bounding box, vehicle kinds, per-minute stats (sim vs real)
//	frames.bin   every -frame of simulated time: each vehicle's position + status
//	pickups.bin  every pickup in the window: time, place, wait, vehicle kind
//
// Binary layout (little-endian). Coordinates are uint16, quantized inside meta.bbox
// (~1 m in NYC). frames.bin is columnar per frame so it compresses well:
//
//	frame := lat[N]u16 lon[N]u16 status[N]u8 nPending:u16 (lat u16, lon u16)[nPending]
//	status: 0 offline, 1 idle, 2 en route to pickup, 3 on trip
//	pickup := t:u16 (s since window start) lat:u16 lon:u16 wait:u16 (s) kind:u8 (0 human, 1 av)
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/dispatch"
	"github.com/Nijjea1/fleetline/core/internal/geo"
	"github.com/Nijjea1/fleetline/core/internal/model"
	"github.com/Nijjea1/fleetline/core/internal/offline"
	"github.com/Nijjea1/fleetline/core/internal/replay"
)

// NYC bounding box (same as the OSRM extract).
var bbox = [4]float64{-74.30, 40.48, -73.68, 40.95} // minLon, minLat, maxLon, maxLat

func q(v, lo, hi float64) uint16 {
	x := (v - lo) / (hi - lo) * 65535
	return uint16(math.Max(0, math.Min(65535, math.Round(x))))
}

type Minute struct {
	T        int     `json:"t"` // s since window start
	Online   int     `json:"online"`
	Busy     int     `json:"busy"`
	Pending  int     `json:"pending"`
	Pickups  int     `json:"pickups"`
	SimP50   float64 `json:"sim_p50_s"`
	RealP50  float64 `json:"real_p50_s"`
	Requests int     `json:"requests"`
	Unserved int     `json:"unserved"`
}

type Meta struct {
	Day          string            `json:"day"`
	From         string            `json:"from"`
	To           string            `json:"to"`
	StartMs      int64             `json:"start_ms"`
	FrameS       int               `json:"frame_s"`
	Frames       int               `json:"frames"`
	Vehicles     int               `json:"vehicles"`
	Kinds        string            `json:"kinds"` // one char per vehicle: h or a
	BBox         [4]float64        `json:"bbox"`
	RobotaxiZone [][2]float64      `json:"robotaxi_zone"`
	Pickups      int               `json:"pickups"`
	Minutes      []Minute          `json:"minutes"`
	Summary      any               `json:"summary"`
	Params       map[string]string `json:"params"`
}

func main() {
	var (
		replayPath = flag.String("replay", "../data/derived/day_2026-09-16_seed7.parquet", "replay day")
		modelPath  = flag.String("model", "../data/derived/travel_model_2026-09-16.json", "travel model")
		supplyFile = flag.String("supply-file", "../bench/calibration/sep16_cap600.json", "per-hour utilization (cmd/calibrate)")
		from       = flag.String("from", "08:00", "window start HH:MM")
		to         = flag.String("to", "08:20", "window end HH:MM")
		frame      = flag.Duration("frame", 10*time.Second, "simulated time between frames")
		maxPickup  = flag.Duration("max-pickup", 600*time.Second, "greedy pickup-ETA cap")
		out        = flag.String("out", "../web/public/replay", "output directory")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	travel, err := geo.LoadTravelModel(*modelPath)
	must(log, err)
	trips, err := replay.Load(*replayPath)
	must(log, err)
	day := time.UnixMilli(trips[0].TS).UTC().Truncate(24 * time.Hour)
	fromMs := day.Add(hhmm(log, *from)).UnixMilli()
	toMs := day.Add(hhmm(log, *to)).UnixMilli()

	var util struct {
		U [24]float64 `json:"utilization_by_hour"`
	}
	b, err := os.ReadFile(*supplyFile)
	must(log, err)
	must(log, json.Unmarshal(b, &util))

	frameMs := frame.Milliseconds()
	slot := map[string]int{}
	var kinds []byte
	type fr struct {
		lat, lon []uint16
		status   []byte
		pending  [][2]uint16
	}
	var frames []fr
	statusCode := map[model.VehicleStatus]byte{model.StatusIdle: 1, model.StatusEnRoute: 2, model.StatusOnTrip: 3}

	cfg := offline.Config{
		Travel: travel,
		Policy: &dispatch.Greedy{Travel: travel, RadiusMi: 3, K: 8, MaxPickupSec: maxPickup.Seconds()},
		From:   fromMs, To: toMs, Warmup: time.Hour, Drain: 30 * time.Minute,
		Patience: 10 * time.Minute, Window: time.Second,
		Supply:  offline.DataSupply{Busy: travel.BusyVehiclesByHour, PerHour: &util.U},
		AVShare: 0.1, Seed: 7,
		Observe: func(t int64, v offline.View) {
			if t < fromMs || t >= toMs || (t-fromMs)%frameMs != 0 {
				return
			}
			for _, veh := range v.Vehicles {
				if _, ok := slot[veh.ID]; !ok && v.Online[veh.ID] {
					slot[veh.ID] = len(kinds)
					k := byte('h')
					if veh.Kind == model.KindAV {
						k = 'a'
					}
					kinds = append(kinds, k)
				}
			}
			f := fr{lat: make([]uint16, len(kinds)), lon: make([]uint16, len(kinds)), status: make([]byte, len(kinds))}
			for _, veh := range v.Vehicles {
				i, ok := slot[veh.ID]
				if !ok || !v.Online[veh.ID] {
					continue
				}
				lat, lon := veh.Position(t)
				f.lat[i], f.lon[i] = q(lat, bbox[1], bbox[3]), q(lon, bbox[0], bbox[2])
				f.status[i] = statusCode[veh.Status]
			}
			for _, r := range v.Pending {
				f.pending = append(f.pending, [2]uint16{q(r.PickLat, bbox[1], bbox[3]), q(r.PickLon, bbox[0], bbox[2])})
			}
			frames = append(frames, f)
		},
	}
	res, err := offline.Run(context.Background(), cfg, trips)
	must(log, err)

	// Frames: vehicles that appeared later are offline (status 0) in earlier frames.
	n := len(kinds)
	var fb bytes.Buffer
	for _, f := range frames {
		for _, col := range [][]uint16{pad16(f.lat, n), pad16(f.lon, n)} {
			must(log, binary.Write(&fb, binary.LittleEndian, col))
		}
		fb.Write(pad8(f.status, n))
		must(log, binary.Write(&fb, binary.LittleEndian, uint16(len(f.pending))))
		for _, p := range f.pending {
			must(log, binary.Write(&fb, binary.LittleEndian, p))
		}
	}

	// Pickups in the window, from the per-trip results + the replay's pickup points.
	where := map[int64][2]float64{}
	for _, t := range trips {
		if t.TS >= fromMs-int64(time.Hour/time.Millisecond) && t.TS < toMs {
			where[t.TripID] = [2]float64{t.PickLat, t.PickLon}
		}
	}
	var pb bytes.Buffer
	minutes := make([]Minute, int((toMs-fromMs)/60_000))
	simW := make([][]float64, len(minutes))
	realW := make([][]float64, len(minutes))
	npick := 0
	for _, tr := range res.Trips {
		if tr.Warmup {
			continue
		}
		if m := int((tr.RequestTS - fromMs) / 60_000); m >= 0 && m < len(minutes) {
			minutes[m].Requests++
			realW[m] = append(realW[m], float64(tr.RealWaitS))
			if tr.Outcome == offline.Served {
				simW[m] = append(simW[m], tr.WaitS)
			} else {
				minutes[m].Unserved++
			}
		}
		if tr.Outcome != offline.Served {
			continue
		}
		pt := tr.RequestTS + int64(tr.WaitS*1000)
		if pt < fromMs || pt >= toMs {
			continue
		}
		p := where[tr.TripID]
		kind := uint8(0)
		if tr.Kind == string(model.KindAV) {
			kind = 1
		}
		must(log, binary.Write(&pb, binary.LittleEndian, struct {
			T, Lat, Lon, Wait uint16
			Kind              uint8
		}{uint16((pt - fromMs) / 1000), q(p[0], bbox[1], bbox[3]), q(p[1], bbox[0], bbox[2]),
			uint16(math.Min(65535, tr.WaitS)), kind}))
		npick++
		if m := int((pt - fromMs) / 60_000); m < len(minutes) {
			minutes[m].Pickups++
		}
	}
	for _, s := range res.Samples {
		if m := int((s.TS - fromMs) / 60_000); s.TS >= fromMs && m < len(minutes) {
			minutes[m].Online, minutes[m].Busy, minutes[m].Pending = s.Online, s.Busy, s.Pending
		}
	}
	for m := range minutes {
		minutes[m].T = m * 60
		minutes[m].SimP50, minutes[m].RealP50 = median(simW[m]), median(realW[m])
	}

	sum := offline.Summarize(res)
	meta := Meta{
		Day: day.Format("2006-01-02"), From: *from, To: *to, StartMs: fromMs,
		FrameS: int(frame.Seconds()), Frames: len(frames), Vehicles: n, Kinds: string(kinds),
		BBox: bbox, RobotaxiZone: geo.RobotaxiZone, Pickups: npick, Minutes: minutes,
		Summary: sum,
		Params: map[string]string{"policy": "greedy", "max_pickup": maxPickup.String(),
			"supply": filepath.Base(*supplyFile), "seed": "7"},
	}
	must(log, os.MkdirAll(*out, 0o755))
	mb, _ := json.MarshalIndent(meta, "", " ")
	must(log, os.WriteFile(filepath.Join(*out, "meta.json"), mb, 0o644))
	must(log, os.WriteFile(filepath.Join(*out, "frames.bin"), fb.Bytes(), 0o644))
	must(log, os.WriteFile(filepath.Join(*out, "pickups.bin"), pb.Bytes(), 0o644))
	log.Info("wrote", "dir", *out, "frames", len(frames), "vehicles", n, "pickups", npick,
		"frames_mb", float64(fb.Len())/1e6, "runtime", res.Elapsed.Round(time.Millisecond))
}

func pad16(x []uint16, n int) []uint16 {
	for len(x) < n {
		x = append(x, 0)
	}
	return x
}

func pad8(x []byte, n int) []byte {
	for len(x) < n {
		x = append(x, 0)
	}
	return x
}

func median(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	sort.Float64s(x)
	m := len(x) / 2
	if len(x)%2 == 1 {
		return x[m]
	}
	return (x[m-1] + x[m]) / 2
}

func hhmm(log *slog.Logger, s string) time.Duration {
	p := strings.Split(s, ":")
	h, err1 := strconv.Atoi(p[0])
	m, err2 := strconv.Atoi(p[1])
	if err1 != nil || err2 != nil || len(p) != 2 {
		log.Error("bad HH:MM", "v", s)
		os.Exit(1)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
}

func must(log *slog.Logger, err error) {
	if err != nil {
		log.Error("export-replay failed", "err", err)
		os.Exit(1)
	}
}
