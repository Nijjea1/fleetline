#pragma once
#include <cstdint>
#include <vector>

struct AuctionStats {
  double setup_s = 0;      // build the CSR on the host + allocate + copy to the GPU
  double solve_s = 0;      // all auction phases, until the GPU is done
  double copy_back_s = 0;  // copy the assignment back to the host
  int phases = 0;
  long long rounds = 0;    // bid/resolve/award rounds over all phases
  long long bids = 0;      // rider bids over all phases
  int blocks = 0;          // cooperative grid size
  int gap_stops = 0;       // 1 if a phase's duality gap certified the optimum before eps = 1
  std::vector<long long> phase_rounds;
  // Rounds and block 0's SM clock cycles with the whole grid bidding vs. block 0 alone (tail).
  long long grid_rounds = 0, tail_rounds = 0, grid_cycles = 0, tail_cycles = 0;
};

// rows/cols/cost_ms: deduplicated edges, costs in whole milliseconds.
// Returns row -> col, or -1 for unmatched.
std::vector<int32_t> auction_solve(int n_rows, int n_cols, const int32_t* rows, const int32_t* cols,
                                   const int64_t* cost_ms, int64_t n_edges, int64_t unmatched_ms, int theta,
                                   AuctionStats* stats);
