package metrics

import "testing"

func TestQuantileMatchesNumpy(t *testing.T) {
	// numpy.quantile([1,2,3,4,5,6,7,8,9,10], [0.5, 0.9]) -> [5.5, 9.1]
	d := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if q := Quantile(d, 0.5); q != 5.5 {
		t.Fatalf("p50 %v", q)
	}
	if q := Quantile(d, 0.9); q < 9.0999 || q > 9.1001 {
		t.Fatalf("p90 %v", q)
	}
}

func TestSnapshot(t *testing.T) {
	s := NewSummary()
	for _, w := range []float64{60, 120, 180} {
		s.Pickup(w)
	}
	s.Expired()
	snap := s.Snapshot()
	if snap.Served != 3 || snap.Expired != 1 || snap.UnservedRate != 0.25 || snap.WaitMeanSec != 120 {
		t.Fatalf("%+v", snap)
	}
}
