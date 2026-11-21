// Package geo holds distance, travel-time and H3 helpers shared by the world simulator
// and the dispatcher.
package geo

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/uber/h3-go/v4"
)

const earthRadiusMi = 3958.8

// HaversineMi is the great-circle ("as the crow flies") distance in miles.
func HaversineMi(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dPhi := p2 - p1
	dLmb := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dLmb/2)*math.Sin(dLmb/2)
	return 2 * earthRadiusMi * math.Asin(math.Sqrt(a))
}

// TravelModel turns straight-line distance into a road travel time. Its parameters are
// fitted from real trips by `fleetline-sim calibrate` (see sim/calibrate.py).
//
// This is deliberately simple: it is the "routing-only" baseline that the Phase 3 ETA
// model (OSRM + PyTorch residual) has to beat.
type TravelModel struct {
	DetourFactor   float64     `json:"detour_factor"`
	SpeedMPHByHour [24]float64 `json:"speed_mph_by_hour"`
}

// LoadTravelModel reads the calibration JSON.
func LoadTravelModel(path string) (TravelModel, error) {
	var m TravelModel
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.DetourFactor < 1 {
		return m, fmt.Errorf("detour factor %.3f < 1 in %s", m.DetourFactor, path)
	}
	for h, s := range m.SpeedMPHByHour {
		if s <= 0 {
			return m, fmt.Errorf("speed for hour %d is %.2f in %s", h, s, path)
		}
	}
	return m, nil
}

// RoadMiles estimates driving distance between two points.
func (m TravelModel) RoadMiles(lat1, lon1, lat2, lon2 float64) float64 {
	return HaversineMi(lat1, lon1, lat2, lon2) * m.DetourFactor
}

// TravelSec estimates driving time in seconds, using the speed for the hour of `at`
// (which must already be in New York local time; replay timestamps are).
func (m TravelModel) TravelSec(lat1, lon1, lat2, lon2 float64, at time.Time) float64 {
	mph := m.SpeedMPHByHour[at.Hour()]
	return m.RoadMiles(lat1, lon1, lat2, lon2) / mph * 3600
}

// SpeedMPS is the modeled speed in miles per second at a given time.
func (m TravelModel) SpeedMPS(at time.Time) float64 {
	return m.SpeedMPHByHour[at.Hour()] / 3600
}

// Interpolate moves `frac` (0..1) of the way from point 1 to point 2. At city scale a
// straight line in lat/lon is indistinguishable from a great circle.
func Interpolate(lat1, lon1, lat2, lon2, frac float64) (float64, float64) {
	if frac >= 1 {
		return lat2, lon2
	}
	return lat1 + (lat2-lat1)*frac, lon1 + (lon2-lon1)*frac
}

// H3 resolutions used across Fleetline.
const (
	PartitionRes = 7 // ~5 km² hexes: Kafka partition key, ~250 cells cover NYC
	CellRes      = 8 // ~0.74 km² hexes: vehicle state and demand bucketing
)

// Cell returns the H3 cell at res as its canonical hex string.
func Cell(lat, lon float64, res int) string {
	c, err := h3.LatLngToCell(h3.LatLng{Lat: lat, Lng: lon}, res)
	if err != nil {
		return ""
	}
	return c.String()
}
