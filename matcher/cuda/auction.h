#pragma once
#include <cstdint>
#include <vector>

struct AuctionStats {
  double setup_s = 0;      // build the square CSR on the host + allocate + copy to the GPU
  double solve_s = 0;      // all auction phases, until the GPU is done
  double copy_back_s = 0;  // copy the assignment back to the host
  int phases = 0;
  long long rounds = 0;    // bid/resolve/award rounds over all phases
};

// rows/cols/cost_ms: deduplicated edges, costs in whole milliseconds.
// Returns row -> col, or -1 for unmatched.
std::vector<int32_t> auction_solve(int n_rows, int n_cols, const int32_t* rows, const int32_t* cols,
                                   const int64_t* cost_ms, int64_t n_edges, int64_t unmatched_ms, int theta,
                                   int check_every, AuctionStats* stats);
