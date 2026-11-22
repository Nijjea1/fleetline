package offline

import (
	"testing"
	"time"
)

func at(h, m int) int64 { return time.Date(2026, 9, 16, h, m, 0, 0, time.UTC).UnixMilli() }

func TestDataSupplyInterpolates(t *testing.T) {
	var d DataSupply
	d.Utilization = 0.5
	d.Busy[7] = 1000 // 07:30
	d.Busy[8] = 2000 // 08:30
	if got := d.Target(at(7, 30)); got != 2000 {
		t.Fatalf("at sample: %d", got)
	}
	if got := d.Target(at(8, 0)); got != 3000 { // halfway: 1500 busy / 0.5
		t.Fatalf("halfway: %d", got)
	}
	if got := d.Target(at(23, 59)); got != 0 { // Busy[23] is 0 in this test
		t.Fatalf("end of day: %d", got)
	}
}
