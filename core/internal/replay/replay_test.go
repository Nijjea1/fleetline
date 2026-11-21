package replay

import (
	"os"
	"testing"
	"time"
)

// Runs against the real replay day when it exists (after `fleetline-sim prepare`).
func TestLoadRealDay(t *testing.T) {
	path := "../../../data/derived/day_2026-09-16_seed7.parquet"
	if _, err := os.Stat(path); err != nil {
		t.Skip("replay day not built; run `uv run fleetline-sim prepare` in sim/")
	}
	trips, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 635_294 {
		t.Fatalf("got %d trips, want 635,294 (same as the Python pipeline)", len(trips))
	}
	first := time.UnixMilli(trips[0].TS).UTC()
	last := time.UnixMilli(trips[len(trips)-1].TS).UTC()
	if first.Format("2006-01-02") != "2026-09-16" || last.Format("2006-01-02") != "2026-09-16" {
		t.Fatalf("trips outside replay day: %v .. %v", first, last)
	}
	for _, tr := range trips[:1000] {
		if tr.PickLat < 40.4 || tr.PickLat > 41 || tr.PickLon > -73.6 || tr.PickLon < -74.3 {
			t.Fatalf("pickup outside NYC: %+v", tr)
		}
	}
}
