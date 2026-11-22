package offline

import "time"

// Supply decides how many vehicles should be online at a given time.
type Supply interface {
	Target(t int64) int
}

// FixedSupply keeps the same fleet all day.
type FixedSupply int

// Target implements Supply.
func (f FixedSupply) Target(int64) int { return int(f) }

// DataSupply follows the real busy-vehicle curve: busy[h] is how many real cars were
// serving a trip at h:30. Online drivers at h:30 = busy[h] / utilization[h]; between
// samples we interpolate linearly. Utilization is the free parameter: either one value
// for the whole day, or one per hour fitted by cmd/calibrate so that greedy's simulated
// waits match the real ones hour by hour.
type DataSupply struct {
	Busy        [24]int
	Utilization float64      // used when PerHour is nil
	PerHour     *[24]float64 // calibrated utilization by hour
}

func (d DataSupply) online(h int) float64 {
	u := d.Utilization
	if d.PerHour != nil {
		u = d.PerHour[h]
	}
	return float64(d.Busy[h]) / u
}

// Target implements Supply.
func (d DataSupply) Target(t int64) int {
	tt := time.UnixMilli(t).UTC()
	// Hours since the first sample (00:30), as a float.
	x := float64(tt.Hour()) + float64(tt.Minute())/60 + float64(tt.Second())/3600 - 0.5
	lo := int(x)
	frac := x - float64(lo)
	if x < 0 { // 00:00-00:30: hold the first sample (no data from the previous day here)
		lo, frac = 0, 0
	}
	hi := lo + 1
	if hi > 23 {
		hi, frac = 23, 0
	}
	return int(d.online(lo)*(1-frac) + d.online(hi)*frac)
}
