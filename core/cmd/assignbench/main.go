// Command assignbench times internal/assign on instance files written by the matcher
// package (matcher/src/fleetline_matcher/instances.py), for E4.
//
//	assignbench -reps 5 -match out.bin instance.bin
//
// Prints JSON {"solve_s": best of reps, "setup_s": ...}; with -match, writes the result
// (int32 per row, -1 = unmatched) so Python can verify it against the other solvers.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/Nijjea1/fleetline/core/internal/assign"
)

const unmatched = 1e6 // must equal matcher's UNMATCHED

func main() {
	reps := flag.Int("reps", 3, "repetitions; the fastest is reported")
	matchOut := flag.String("match", "", "write the assignment here (int32 per row)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: assignbench [-reps n] [-match out.bin] instance.bin")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *reps, *matchOut); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, reps int, matchOut string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	le := binary.LittleEndian
	nRows, nCols, nEdges := int(le.Uint32(raw)), int(le.Uint32(raw[4:])), int(le.Uint32(raw[8:]))
	if len(raw) != 12+16*nEdges {
		return fmt.Errorf("%s: size %d doesn't match %d edges", path, len(raw), nEdges)
	}
	t0 := time.Now()
	edges := make([]assign.Edge, nEdges)
	rows, cols, costs := raw[12:], raw[12+4*nEdges:], raw[12+8*nEdges:]
	for i := range edges {
		edges[i] = assign.Edge{
			Row:  int(int32(le.Uint32(rows[4*i:]))),
			Col:  int(int32(le.Uint32(cols[4*i:]))),
			Cost: math.Float64frombits(le.Uint64(costs[8*i:])),
		}
	}
	setup := time.Since(t0)

	best := time.Duration(math.MaxInt64)
	var match []int
	for range max(1, reps) {
		t := time.Now()
		match = assign.Solve(nRows, nCols, edges, unmatched)
		best = min(best, time.Since(t))
	}
	if matchOut != "" {
		buf := make([]byte, 4*len(match))
		for i, c := range match {
			le.PutUint32(buf[4*i:], uint32(int32(c)))
		}
		if err := os.WriteFile(matchOut, buf, 0o644); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]float64{"solve_s": best.Seconds(), "setup_s": setup.Seconds()})
}
