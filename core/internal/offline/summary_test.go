package offline

import "testing"

func TestSummarizeExcludesWarmupAndComparesSameRequests(t *testing.T) {
	res := &Result{Trips: []TripResult{
		{TripID: 1, RequestTS: at(6, 50), Warmup: true, Outcome: Served, WaitS: 999, RealWaitS: 999, Kind: "human"},
		{TripID: 2, RequestTS: at(7, 5), Outcome: Served, WaitS: 100, RealWaitS: 300, Kind: "human"},
		{TripID: 3, RequestTS: at(7, 10), Outcome: Served, WaitS: 200, RealWaitS: 400, Kind: "av"},
		{TripID: 4, RequestTS: at(8, 0), Outcome: Expired, RealWaitS: 500},
	}}
	s := Summarize(res)
	if s.Requests != 3 || s.Served != 2 || s.Expired != 1 {
		t.Fatalf("counts %+v", s)
	}
	if s.Sim.Mean != 150 {
		t.Fatalf("sim mean %v", s.Sim.Mean)
	}
	// Real stats include the expired request too (same set of riders).
	if s.Real.N != 3 || s.Real.Mean != 400 {
		t.Fatalf("real %+v", s.Real)
	}
	if len(s.ByHour) != 2 || s.ByHour[0].Hour != 7 || s.ByHour[1].Unserved != 1 {
		t.Fatalf("by hour %+v", s.ByHour)
	}
	if s.ByKind["av"] != 1 || s.ByKind["human"] != 1 {
		t.Fatalf("by kind %+v", s.ByKind)
	}
}
