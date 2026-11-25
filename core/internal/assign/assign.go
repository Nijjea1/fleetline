// Package assign solves sparse rectangular assignment problems: match rows (riders) to
// columns (vehicles), each column at most once, minimizing total cost.
//
// Dispatch graphs are sparse: a rider only links to their K nearest cars, not to all
// 29,000. A dense Hungarian solver would need an n x m matrix; this works on the edge
// list directly, using successive shortest paths: add riders one at a time, and each time
// find the cheapest way to fit the new rider in, possibly re-routing earlier riders to
// other cars along an "augmenting path". Dijkstra finds that path; node potentials keep
// edge costs non-negative so Dijkstra stays valid even though re-routing creates
// negative-cost backward edges. The result is exactly optimal.
//
// Every row also has a private "stay unmatched" option costing Unmatched. With Unmatched
// larger than any possible total cost difference, the solver first matches as many rows
// as it can, then minimizes cost among those matchings.
package assign

import "container/heap"

// Edge says row Row may take column Col at Cost (>= 0).
type Edge struct {
	Row, Col int
	Cost     float64
}

// Solve returns, for each of nRows rows, the column it gets, or -1 if it stays
// unmatched. Columns are 0..nCols-1. Deterministic: ties break by node index.
func Solve(nRows, nCols int, edges []Edge, unmatched float64) []int {
	type arc struct {
		col  int
		cost float64
	}
	adj := make([][]arc, nRows)
	for _, e := range edges {
		adj[e.Row] = append(adj[e.Row], arc{e.Col, e.Cost})
	}
	// Node ids: rows 0..nRows-1, real columns nRows..nRows+nCols-1, then one dummy column
	// per row ("unmatched").
	colNode := func(c int) int { return nRows + c }
	dummy := func(r int) int { return nRows + nCols + r }
	for r := range adj {
		adj[r] = append(adj[r], arc{nCols + r, unmatched})
	}
	nNodes := nRows + nCols + nRows

	pot := make([]float64, nNodes)
	matchOfCol := make([]int, nNodes) // column node -> row, -1 if free
	for i := range matchOfCol {
		matchOfCol[i] = -1
	}
	colOfRow := make([]int, nRows) // row -> column node, -1 if none yet
	costOfRow := make([]float64, nRows)
	for i := range colOfRow {
		colOfRow[i] = -1
	}

	dist := make([]float64, nNodes)
	seen := make([]bool, nNodes)  // dist[] valid this round
	done := make([]bool, nNodes)  // finalized by Dijkstra this round
	parent := make([]int, nNodes) // column node -> row it was reached from
	edgeCost := make([]float64, nNodes)
	var touched, finalized []int
	pq := &nodeHeap{}

	for s := 0; s < nRows; s++ {
		for _, x := range touched {
			seen[x], done[x] = false, false
		}
		touched, finalized = touched[:0], finalized[:0]
		*pq = (*pq)[:0]

		relax := func(x int, d float64) {
			if !seen[x] || d < dist[x] {
				if !seen[x] {
					touched = append(touched, x)
				}
				seen[x], dist[x] = true, d
				heap.Push(pq, item{d, x})
			}
		}
		relax(s, 0)
		sink := -1
		for pq.Len() > 0 {
			it := heap.Pop(pq).(item)
			x := it.node
			if done[x] || it.d > dist[x] {
				continue
			}
			done[x] = true
			finalized = append(finalized, x)
			if x < nRows { // a row: try each of its columns (except the one it holds)
				for _, a := range adj[x] {
					c := colNode(a.col)
					if c == colOfRow[x] || done[c] {
						continue
					}
					d := dist[x] + a.cost + pot[x] - pot[c]
					if !seen[c] || d < dist[c] {
						parent[c], edgeCost[c] = x, a.cost
					}
					relax(c, d)
				}
				continue
			}
			r := matchOfCol[x]
			if r < 0 { // a free column: the cheapest augmenting path ends here
				sink = x
				break
			}
			// A taken column: its row could move elsewhere (backward edge, cost -cost).
			if !done[r] {
				relax(r, dist[x]-costOfRow[r]+pot[x]-pot[r])
			}
		}

		// Keep reduced costs non-negative for the next round. Shifting every potential by
		// the same constant changes nothing, so only finalized nodes need updating.
		D := dist[sink]
		for _, x := range finalized {
			if dist[x] < D {
				pot[x] += dist[x] - D
			}
		}

		// Flip the path: each row on it takes the column it was reached through.
		for c := sink; ; {
			r := parent[c]
			prev := colOfRow[r]
			colOfRow[r], costOfRow[r], matchOfCol[c] = c, edgeCost[c], r
			if prev >= 0 {
				matchOfCol[prev] = -1
			}
			if r == s {
				break
			}
			c = prev
		}
	}

	out := make([]int, nRows)
	for r, c := range colOfRow {
		if c >= dummy(0) || c < 0 {
			out[r] = -1
		} else {
			out[r] = c - nRows
		}
	}
	return out
}

type item struct {
	d    float64
	node int
}

type nodeHeap []item

func (h nodeHeap) Len() int { return len(h) }
func (h nodeHeap) Less(i, j int) bool {
	if h[i].d != h[j].d {
		return h[i].d < h[j].d
	}
	return h[i].node < h[j].node
}
func (h nodeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *nodeHeap) Push(x any)   { *h = append(*h, x.(item)) }
func (h *nodeHeap) Pop() any {
	old := *h
	it := old[len(old)-1]
	*h = old[:len(old)-1]
	return it
}
