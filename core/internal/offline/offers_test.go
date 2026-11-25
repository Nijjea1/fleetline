package offline

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"
)

func withOffers(fleet int) Config {
	c := cfg(fleet)
	c.Offers = Offers{Delay: 10 * time.Second, DeclineBase: 0.1, DeclinePerMin: 0.05}
	return c
}

func TestDeclineProbability(t *testing.T) {
	o := Offers{DeclineBase: 0.1, DeclinePerMin: 0.05, DeclineMax: 0.5}
	for _, c := range []struct {
		eta  int64
		want float64
	}{{0, 0.1}, {120, 0.2}, {600, 0.5}, {3600, 0.5}} {
		if got := o.declineP(c.eta); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("eta %ds: got %v want %v", c.eta, got, c.want)
		}
	}
	if (Offers{}).on() {
		t.Error("zero value must be off")
	}
}

// Offers on: still deterministic, every request still gets exactly one outcome, the
// decline rate is in the range the parameters imply, and riders wait longer.
func TestOffersSlowRidersButKeepInvariants(t *testing.T) {
	trips := synthetic(2000, 3)
	a, err := Run(context.Background(), withOffers(600), trips)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Run(context.Background(), withOffers(600), trips)
	if !reflect.DeepEqual(a.Trips, b.Trips) {
		t.Fatal("offers must be deterministic for a fixed seed")
	}
	seen := map[int64]bool{}
	for _, tr := range a.Trips {
		if seen[tr.TripID] {
			t.Fatalf("trip %d reported twice", tr.TripID)
		}
		seen[tr.TripID] = true
	}
	if a.OffersMade == 0 {
		t.Fatal("no offers made")
	}
	rate := float64(a.Declined) / float64(a.OffersMade)
	if rate < 0.1 || rate > 0.6 {
		t.Fatalf("decline rate %.3f outside the parameters' range", rate)
	}

	base, _ := Run(context.Background(), cfg(600), trips)
	on, off := Summarize(a), Summarize(base)
	if on.Sim.Mean <= off.Sim.Mean {
		t.Fatalf("offers should add waiting: mean %.1f s with vs %.1f s without", on.Sim.Mean, off.Sim.Mean)
	}
	if on.UnservedRate > 0.01 || off.UnservedRate > 0.01 {
		t.Fatalf("fleet should be ample here (unserved %.3f / %.3f): the wait comparison needs it", on.UnservedRate, off.UnservedRate)
	}
	if off.OffersMade != 0 {
		t.Fatal("offers made with the model off")
	}
}

// Robotaxis never get an offer: with an all-AV fleet the model changes nothing.
func TestRobotaxisAcceptInstantly(t *testing.T) {
	trips := synthetic(1000, 4)
	c := withOffers(150)
	c.AVShare = 1
	on, err := Run(context.Background(), c, trips)
	if err != nil {
		t.Fatal(err)
	}
	c.Offers = Offers{}
	off, _ := Run(context.Background(), c, trips)
	if on.OffersMade != 0 || !reflect.DeepEqual(on.Trips, off.Trips) {
		t.Fatalf("all-AV run changed with offers on (%d offers)", on.OffersMade)
	}
}
