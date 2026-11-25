package assign

import (
	"math"
	"math/rand/v2"
	"testing"
)

const big = 1e6

// total is the objective Solve minimizes: matched edge costs plus Unmatched per free row.
func total(nRows int, edges []Edge, got []int, unmatched float64) (float64, bool) {
	cost := map[[2]int]float64{}
	for _, e := range edges {
		if c, ok := cost[[2]int{e.Row, e.Col}]; !ok || e.Cost < c {
			cost[[2]int{e.Row, e.Col}] = e.Cost
		}
	}
	used := map[int]bool{}
	sum := 0.0
	for r := 0; r < nRows; r++ {
		c := got[r]
		if c < 0 {
			sum += unmatched
			continue
		}
		w, ok := cost[[2]int{r, c}]
		if !ok || used[c] {
			return 0, false // not an edge, or column used twice: invalid
		}
		used[c] = true
		sum += w
	}
	return sum, true
}

// brute tries every assignment (each row: one of its columns or unmatched).
func brute(nRows int, edges []Edge, unmatched float64) float64 {
	byRow := make([][]Edge, nRows)
	for _, e := range edges {
		byRow[e.Row] = append(byRow[e.Row], e)
	}
	best := math.Inf(1)
	used := map[int]bool{}
	var rec func(r int, sum float64)
	rec = func(r int, sum float64) {
		if sum >= best {
			return
		}
		if r == nRows {
			best = sum
			return
		}
		rec(r+1, sum+unmatched)
		for _, e := range byRow[r] {
			if !used[e.Col] {
				used[e.Col] = true
				rec(r+1, sum+e.Cost)
				used[e.Col] = false
			}
		}
	}
	rec(0, 0)
	return best
}

func TestMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 2000; trial++ {
		nRows, nCols := 1+rng.IntN(6), 1+rng.IntN(6)
		var edges []Edge
		for r := 0; r < nRows; r++ {
			for c := 0; c < nCols; c++ {
				if rng.Float64() < 0.5 {
					// Small integer costs make ties common, which is where bugs hide.
					edges = append(edges, Edge{r, c, float64(rng.IntN(10))})
				}
			}
		}
		// Some trials use a cheap "unmatched" so leaving a row free is sometimes optimal.
		unmatched := big
		if trial%3 == 0 {
			unmatched = float64(rng.IntN(12))
		}
		got := Solve(nRows, nCols, edges, unmatched)
		sum, ok := total(nRows, edges, got, unmatched)
		if !ok {
			t.Fatalf("trial %d: invalid assignment %v for edges %v", trial, got, edges)
		}
		if want := brute(nRows, edges, unmatched); math.Abs(sum-want) > 1e-9 {
			t.Fatalf("trial %d: cost %v, optimum %v (got %v, edges %v)", trial, sum, want, got, edges)
		}
	}
}

// The case greedy gets wrong: rider 0 grabs car A, the only car rider 1 can use.
func TestBeatsGreedy(t *testing.T) {
	edges := []Edge{
		{0, 0, 1}, // rider 0 -> A (closest)
		{0, 1, 2}, // rider 0 -> B
		{1, 0, 3}, // rider 1 -> A only
	}
	got := Solve(2, 2, edges, big)
	if got[0] != 1 || got[1] != 0 {
		t.Fatalf("want rider0->B, rider1->A, got %v", got)
	}
}

// Needs an augmenting path through two earlier riders.
func TestLongAugmentingPath(t *testing.T) {
	edges := []Edge{
		{0, 0, 1}, {0, 1, 5},
		{1, 1, 1}, {1, 2, 5},
		{2, 0, 2}, // rider 2 can only use car 0
	}
	got := Solve(3, 3, edges, big)
	if got[0] != 1 || got[1] != 2 || got[2] != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestEmptyAndUnreachable(t *testing.T) {
	if got := Solve(0, 0, nil, big); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	got := Solve(2, 1, []Edge{{0, 0, 4}}, big)
	if got[0] != 0 || got[1] != -1 {
		t.Fatalf("got %v", got)
	}
}

// Dispatch-sized: 300 riders x 8 candidates each among 1,500 cars.
func BenchmarkSolve300x8(b *testing.B) {
	rng := rand.New(rand.NewPCG(3, 4))
	var edges []Edge
	for r := 0; r < 300; r++ {
		for k := 0; k < 8; k++ {
			edges = append(edges, Edge{r, rng.IntN(1500), rng.Float64() * 600})
		}
	}
	for b.Loop() {
		Solve(300, 1500, edges, big)
	}
}
