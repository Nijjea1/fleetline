// Package replay loads a replay day built by `fleetline-sim prepare`.
//
// Time convention: replay timestamps are New York local wall-clock times stored without a
// time zone. We keep them that way: event time = those wall-clock fields read as if they
// were UTC. So time.UnixMilli(ts).UTC().Hour() is the local NYC hour, which is what the
// hourly speed model needs. One replay day never crosses a DST change, so nothing breaks.
package replay

import (
	"fmt"
	"os"
	"sort"

	"github.com/parquet-go/parquet-go"

	"github.com/Nijjea1/fleetline/core/internal/model"
)

// row mirrors the columns we need from the parquet file. Unlisted columns are skipped,
// and because parquet is columnar they are never even decoded.
type row struct {
	TripID    uint32  `parquet:"trip_id"`
	RequestTS int64   `parquet:"request_ts"` // microseconds
	PuLat     float64 `parquet:"pu_lat"`
	PuLon     float64 `parquet:"pu_lon"`
	DoLat     float64 `parquet:"do_lat"`
	DoLon     float64 `parquet:"do_lon"`
	TripTimeS int64   `parquet:"trip_time_s"`
	RealWaitS int64   `parquet:"real_wait_s"`
}

// Trip is one replayed ride request plus the real-world outcome for comparison.
type Trip struct {
	model.RideRequest
	RealWaitS int64 // what Uber/Lyft actually achieved for this request
}

// Load reads every trip, sorted by request time.
func Load(path string) ([]Trip, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := parquet.Read[row](f, statSize(f))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	trips := make([]Trip, len(rows))
	for i, r := range rows {
		trips[i] = Trip{
			RideRequest: model.RideRequest{
				TripID: int64(r.TripID), TS: r.RequestTS / 1000,
				PickLat: r.PuLat, PickLon: r.PuLon, DropLat: r.DoLat, DropLon: r.DoLon,
				TripTime: r.TripTimeS,
			},
			RealWaitS: r.RealWaitS,
		}
	}
	sort.SliceStable(trips, func(i, j int) bool { return trips[i].TS < trips[j].TS })
	return trips, nil
}

func statSize(f *os.File) int64 {
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	return st.Size()
}
