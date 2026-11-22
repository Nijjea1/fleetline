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
// serving a trip at h:30. Between samples we interpolate linearly, then divide by the
// assumed utilization to include idle cars. Utilization is the one free parameter, and
// it is calibrated so simulated waits match real ones (see bench/calibrate_supply).
type DataSupply struct {
	Busy        [24]int
	Utilization float64
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
	busy := float64(d.Busy[lo])*(1-frac) + float64(d.Busy[hi])*frac
	return int(busy / d.Utilization)
}
