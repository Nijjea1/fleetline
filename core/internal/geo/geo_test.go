package geo

import (
	"math"
	"testing"
	"time"
)

func TestHaversineMatchesPython(t *testing.T) {
	// Same pair as sim/tests/test_calibrate.py: Times Square -> Empire State Building.
	d := HaversineMi(40.7580, -73.9855, 40.7484, -73.9857)
	if math.Abs(d-0.663) > 0.01 {
		t.Fatalf("got %.4f mi, want ~0.663", d)
	}
}

func TestTravelSecUsesHourlySpeed(t *testing.T) {
	var m TravelModel
	m.DetourFactor = 1.0
	for h := range m.SpeedMPHByHour {
		m.SpeedMPHByHour[h] = 10
	}
	m.SpeedMPHByHour[17] = 5 // rush hour: half speed
	lat2 := 40.0 + 1.0/69.0  // ~1 mile north
	noon := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	rush := time.Date(2026, 9, 16, 17, 30, 0, 0, time.UTC)
	a := m.TravelSec(40, -74, lat2, -74, noon)
	b := m.TravelSec(40, -74, lat2, -74, rush)
	if math.Abs(b/a-2) > 1e-9 {
		t.Fatalf("rush hour should take 2x as long: noon %.1fs rush %.1fs", a, b)
	}
}

func TestInterpolateEndpoints(t *testing.T) {
	lat, lon := Interpolate(1, 2, 3, 4, 0)
	if lat != 1 || lon != 2 {
		t.Fatal("frac 0 should return start")
	}
	lat, lon = Interpolate(1, 2, 3, 4, 1.5)
	if lat != 3 || lon != 4 {
		t.Fatal("frac >= 1 should clamp to end")
	}
}

func TestCellResolution(t *testing.T) {
	c7 := Cell(40.758, -73.9855, PartitionRes)
	c8 := Cell(40.758, -73.9855, CellRes)
	if c7 == "" || c8 == "" || c7 == c8 {
		t.Fatalf("unexpected cells %q %q", c7, c8)
	}
}

func TestLoadTravelModelRejectsBadData(t *testing.T) {
	p := t.TempDir() + "/m.json"
	if err := writeFile(p, `{"detour_factor":0.5,"speed_mph_by_hour":[]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTravelModel(p); err == nil {
		t.Fatal("expected error for detour factor < 1")
	}
}
