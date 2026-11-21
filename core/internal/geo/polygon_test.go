package geo

import "testing"

func TestRobotaxiZone(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
		in       bool
	}{
		{"Times Square", 40.7580, -73.9855, true},
		{"Union Square", 40.7359, -73.9911, true},
		{"Wall Street", 40.7060, -74.0090, true},
		{"Harlem 125th", 40.8090, -73.9480, false},
		{"Williamsburg", 40.7081, -73.9571, false},
		{"Jersey City", 40.7178, -74.0431, false},
		{"JFK", 40.6413, -73.7781, false},
	}
	for _, c := range cases {
		if got := RobotaxiZone.Contains(c.lat, c.lon); got != c.in {
			t.Errorf("%s: got %v, want %v", c.name, got, c.in)
		}
	}
}
