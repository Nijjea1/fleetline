// E4 GPU solver: forward auction with epsilon-scaling (Bertsekas) for the sparse
// rider -> car assignment. See matcher/README.md, docs/learning/10-gpu-auction.md, D-018.
//
// The problem is rectangular (n riders, m cars, each rider may stay unmatched at a fixed
// cost). Forward auction is only guaranteed optimal on square problems, so we solve an
// equivalent square one with N = n + m persons and N objects:
//   persons: riders 0..n-1, then one "slack" person per car (n + j)
//   objects: cars 0..m-1, then one "unmatched" object per rider (m + i)
//   rider i  -> car j      benefit -cost_ij      (its real edges)
//   rider i  -> dummy m+i  benefit -UNMATCHED    (stay unmatched)
//   slack j  -> car j      benefit 0             (car j stays idle)
//   slack j  -> dummy m+i  benefit 0             for every rider i with an edge to car j
// A matched pair (i, j) frees dummy m+i, which slack j (adjacent to i via j) takes, so every
// rider assignment extends to a perfect matching with the same cost and vice versa.
//
// Exactness. Costs are whole milliseconds times K = N + 1. Auction ends within N * eps of
// the optimum, so the last phase at eps = 1 is off by less than K, i.e. less than one
// millisecond: it is the exact optimum of the millisecond-rounded problem. We usually stop
// sooner: after each phase the duality gap (sum over persons of best value minus value
// of the assigned object; every object is assigned) bounds the distance to the optimum,
// and every total is a multiple of K, so a gap below K certifies the current assignment.
//
// Phases. Prices carry over between eps phases, and so do assignments that still satisfy
// eps-complementary slackness at the new eps (any such start is valid on a square problem);
// only the persons that break it bid again.
//
// Speed. Each phase is one cooperative kernel: grid-wide barriers between the bid /
// resolve / award steps, no launches or host round trips per round. Late in a phase only
// a few persons are still bidding (one round per step of an eviction chain), so once the
// worklist fits in one block, block 0 finishes the phase alone with cheap block barriers
// while the rest of the grid exits. One launch per phase (not per solve) keeps each
// kernel short: Windows (and so WSL) kills GPU kernels that run for more than ~2 s.
#include "auction.h"

#include <cooperative_groups.h>
#include <cuda_runtime.h>

#include <algorithm>
#include <chrono>
#include <climits>
#include <stdexcept>
#include <string>

namespace cg = cooperative_groups;

namespace {

constexpr int THREADS = 256;
constexpr int SMALL = THREADS * 4;  // worklist size at which block 0 takes over
constexpr int MAX_PHASES = 64;
constexpr long long NEG = LLONG_MIN / 4;
// Extra stats after the per-phase rounds: rounds and SM clock cycles (block 0's clock) spent
// with the whole grid bidding vs. block 0 alone finishing the tail.
constexpr int STAT_GRID_ROUNDS = 2 + MAX_PHASES, STAT_TAIL_ROUNDS = STAT_GRID_ROUNDS + 1,
              STAT_GRID_CYCLES = STAT_GRID_ROUNDS + 2, STAT_TAIL_CYCLES = STAT_GRID_ROUNDS + 3, N_STATS = STAT_GRID_ROUNDS + 4;

#define CK(x)                                                                              \
  do {                                                                                     \
    cudaError_t e_ = (x);                                                                  \
    if (e_ != cudaSuccess)                                                                 \
      throw std::runtime_error(std::string(#x) + ": " + cudaGetErrorString(e_));           \
  } while (0)

double now() {
  return std::chrono::duration<double>(std::chrono::steady_clock::now().time_since_epoch()).count();
}

struct Params {
  int N;                 // persons = objects
  const int* off;        // CSR by person: off[N + 1]
  const int* obj;        // object per edge
  const long long* ben;  // benefit per edge (scaled, <= 0)
  long long lone_incr;   // bid increment for a person with a single option
  long long* price;      // per object
  int* owner;            // object -> person or -1
  int* assign;           // person -> object or -1
  long long* bid_val;    // per person
  int* bid_obj;          // per person
  long long* obj_best;   // per object: highest bid this round
  int* obj_win;          // per object: lowest person id among the highest bidders
  int* wl;               // two worklists of N persons
  int* cnt;              // [0..1] worklist lengths
  long long* stats;      // [0] rounds, [1] bids, [2 + phase] rounds in that phase, then STAT_* below
};

// Best and second-best value (benefit - price) over a person's options.
__device__ inline void top2(const Params& P, int i, long long& v1, long long& v2, int& j1) {
  v1 = NEG;
  v2 = NEG;
  j1 = -1;
  for (int e = P.off[i]; e < P.off[i + 1]; ++e) {
    long long v = P.ben[e] - P.price[P.obj[e]];
    if (v > v1) {
      v2 = v1;
      v1 = v;
      j1 = P.obj[e];
    } else if (v > v2) {
      v2 = v;
    }
  }
}

__device__ inline void push(const Params& P, int out, int i) {
  P.wl[(size_t)out * P.N + atomicAdd(&P.cnt[out], 1)] = i;
}

// One auction round over worklist entries t0, t0 + step, ... < n. `sync` is a grid or
// block barrier.
template <class Sync>
__device__ void round(const Params& P, int in, int out, int n, int t0, int step, long long eps, Sync sync) {
  const int* wl_in = P.wl + (size_t)in * P.N;
  // Bid: the price that makes the person indifferent between its two best options, plus eps.
  for (int t = t0; t < n; t += step) {
    int i = wl_in[t];
    long long v1, v2;
    int j1;
    top2(P, i, v1, v2, j1);
    long long b = P.price[j1] + (v2 == NEG ? P.lone_incr : v1 - v2) + eps;
    P.bid_obj[i] = j1;
    P.bid_val[i] = b;
    atomicMax(&P.obj_best[j1], b);
  }
  sync();
  // Ties on the highest bid go to the lowest person id (deterministic).
  for (int t = t0; t < n; t += step) {
    int i = wl_in[t];
    int j = P.bid_obj[i];
    if (P.bid_val[i] == P.obj_best[j]) atomicMin(&P.obj_win[j], i);
  }
  sync();
  // Award: winners take the object at their bid; evicted owners and losers bid again.
  // Only the unique winner touches its object, so it also resets the object's round state.
  for (int t = t0; t < n; t += step) {
    int i = wl_in[t];
    int j = P.bid_obj[i];
    if (P.obj_win[j] == i) {
      int old = P.owner[j];
      P.owner[j] = i;
      P.assign[i] = j;
      P.price[j] = P.bid_val[i];
      P.obj_best[j] = 0;
      P.obj_win[j] = INT_MAX;
      if (old >= 0) {
        P.assign[old] = -1;
        push(P, out, old);
      }
    } else {
      push(P, out, i);
    }
  }
  sync();
}

__global__ void phase_kernel(Params P, int phase, long long eps) {
  cg::grid_group g = cg::this_grid();
  cg::thread_block blk = cg::this_thread_block();
  const int tid = g.thread_rank(), nthreads = g.size();
  const bool lead = tid == 0;

  if (phase == 0) {
    for (int t = tid; t < P.N; t += nthreads) {
      P.price[t] = 0;
      P.owner[t] = -1;
      P.assign[t] = -1;
      P.obj_best[t] = 0;
      P.obj_win[t] = INT_MAX;
    }
  } else {
    // Keep assignments that satisfy eps-CS at the new eps; free the rest.
    for (int i = tid; i < P.N; i += nthreads) {
      int a = P.assign[i];
      long long v1, v2, cur = NEG;
      int j1;
      top2(P, i, v1, v2, j1);
      for (int e = P.off[i]; e < P.off[i + 1]; ++e)
        if (P.obj[e] == a) cur = P.ben[e] - P.price[a];
      if (cur < v1 - eps) {
        P.owner[a] = -1;
        P.assign[i] = -1;
      }
    }
  }
  if (lead) P.cnt[0] = P.cnt[1] = 0;
  g.sync();
  for (int i = tid; i < P.N; i += nthreads)
    if (P.assign[i] < 0) push(P, 0, i);
  g.sync();

  long long rounds = 0, tail_rounds = 0;
  long long t_start = clock64(), t_tail = 0;
  for (int r = 0;; ++r) {
    int in = r & 1, out = in ^ 1;
    int n = *(volatile int*)&P.cnt[in];
    if (n == 0) break;
    if (n <= SMALL) {
      // Few bidders left: block 0 finishes the phase alone, the rest of the grid exits.
      if (blockIdx.x != 0) return;
      t_tail = clock64();
      for (;; ++r) {
        in = r & 1, out = in ^ 1;
        n = *(volatile int*)&P.cnt[in];
        if (n == 0) break;
        if (threadIdx.x == 0) {
          P.cnt[out] = 0;  // last read a round ago; cleared before anyone can award
          P.stats[1] += n;
        }
        round(P, in, out, n, threadIdx.x, blockDim.x, eps, [&] { blk.sync(); });
        ++rounds;
        ++tail_rounds;
      }
      break;
    }
    if (lead) {
      P.cnt[out] = 0;
      P.stats[1] += n;
    }
    round(P, in, out, n, tid, nthreads, eps, [&] { g.sync(); });
    ++rounds;
  }
  if (lead) {
    long long t_end = clock64();
    if (!t_tail) t_tail = t_end;
    P.stats[0] += rounds;
    P.stats[2 + phase] = rounds;
    P.stats[STAT_GRID_ROUNDS] += rounds - tail_rounds;
    P.stats[STAT_TAIL_ROUNDS] += tail_rounds;
    P.stats[STAT_GRID_CYCLES] += t_tail - t_start;
    P.stats[STAT_TAIL_CYCLES] += t_end - t_tail;
  }
}

// Duality gap of the current assignment: sum over persons of (best value - own value).
__global__ void gap_kernel(Params P, unsigned long long* gap) {
  unsigned long long local = 0;
  for (int i = blockIdx.x * blockDim.x + threadIdx.x; i < P.N; i += gridDim.x * blockDim.x) {
    long long v1, v2, cur = NEG;
    int j1;
    top2(P, i, v1, v2, j1);
    int a = P.assign[i];
    for (int e = P.off[i]; e < P.off[i + 1]; ++e)
      if (P.obj[e] == a) cur = P.ben[e] - P.price[a];
    local += (unsigned long long)(v1 - cur);
  }
  atomicAdd(gap, local);
}

template <class T>
T* upload(const std::vector<T>& v) {
  T* p;
  CK(cudaMalloc(&p, std::max<size_t>(1, v.size()) * sizeof(T)));
  if (!v.empty()) CK(cudaMemcpy(p, v.data(), v.size() * sizeof(T), cudaMemcpyHostToDevice));
  return p;
}

template <class T>
T* alloc(size_t n) {
  T* p;
  CK(cudaMalloc(&p, std::max<size_t>(1, n) * sizeof(T)));
  return p;
}

struct Buffers {
  std::vector<void*> ptrs;
  template <class T>
  T* keep(T* p) {
    ptrs.push_back((void*)p);
    return p;
  }
  ~Buffers() {
    for (void* p : ptrs) cudaFree(p);
  }
};

}  // namespace

std::vector<int32_t> auction_solve(int n_rows, int n_cols, const int32_t* rows, const int32_t* cols,
                                   const int64_t* cost_ms, int64_t n_edges, int64_t unmatched_ms, int theta,
                                   AuctionStats* st) {
  AuctionStats s{};
  std::vector<int32_t> match(n_rows, -1);
  if (n_rows == 0) {
    if (st) *st = s;
    return match;
  }
  double t0 = now();
  const int n = n_rows, m = n_cols, N = n + m;
  const long long K = (long long)N + 1;

  // Build the square problem's CSR on the host (edges are already deduplicated).
  std::vector<int> off(N + 1, 0);
  for (int64_t e = 0; e < n_edges; ++e) {
    off[rows[e] + 1]++;
    off[n + cols[e] + 1]++;
  }
  for (int p = 0; p < N; ++p) off[p + 1] += off[p] + 1;  // +1: own dummy (rider) or own car (slack)
  std::vector<int> obj(off[N]);
  std::vector<long long> ben(off[N]);
  std::vector<int> pos(off.begin(), off.end() - 1);
  const long long cmax = unmatched_ms * K;  // largest |benefit|
  for (int64_t e = 0; e < n_edges; ++e) {
    int i = rows[e], j = cols[e];
    obj[pos[i]] = j;
    ben[pos[i]++] = -cost_ms[e] * K;
    obj[pos[n + j]] = m + i;
    ben[pos[n + j]++] = 0;
  }
  for (int i = 0; i < n; ++i) {
    obj[pos[i]] = m + i;
    ben[pos[i]++] = -unmatched_ms * K;
  }
  for (int j = 0; j < m; ++j) {
    obj[pos[n + j]] = j;
    ben[pos[n + j]++] = 0;
  }

  Buffers b;
  Params P{};
  P.N = N;
  P.off = b.keep(upload(off));
  P.obj = b.keep(upload(obj));
  P.ben = b.keep(upload(ben));
  P.lone_incr = cmax;
  P.price = b.keep(alloc<long long>(N));
  P.owner = b.keep(alloc<int>(N));
  P.assign = b.keep(alloc<int>(N));
  P.bid_val = b.keep(alloc<long long>(N));
  P.bid_obj = b.keep(alloc<int>(N));
  P.obj_best = b.keep(alloc<long long>(N));
  P.obj_win = b.keep(alloc<int>(N));
  P.wl = b.keep(alloc<int>(2 * (size_t)N));
  P.cnt = b.keep(alloc<int>(2));
  P.stats = b.keep(alloc<long long>(N_STATS));
  CK(cudaMemset(P.stats, 0, N_STATS * sizeof(long long)));

  // A cooperative launch needs every block resident at once.
  int dev, sms, per_sm;
  CK(cudaGetDevice(&dev));
  CK(cudaDeviceGetAttribute(&sms, cudaDevAttrMultiProcessorCount, dev));
  CK(cudaOccupancyMaxActiveBlocksPerMultiprocessor(&per_sm, phase_kernel, THREADS, 0));
  const int blocks = std::max(1, std::min(sms * per_sm, (N + THREADS - 1) / THREADS));
  CK(cudaDeviceSynchronize());
  s.setup_s = now() - t0;

  double t1 = now();
  long long eps = std::max(1LL, cmax / theta);
  unsigned long long* d_gap = b.keep(alloc<unsigned long long>(1));
  for (int phase = 0; phase < MAX_PHASES; ++phase) {
    void* args[] = {&P, &phase, &eps};
    CK(cudaLaunchCooperativeKernel((void*)phase_kernel, blocks, THREADS, args, 0, 0));
    s.phases++;
    if (eps == 1) break;
    unsigned long long gap = 0;
    CK(cudaMemsetAsync(d_gap, 0, sizeof(gap)));
    gap_kernel<<<blocks, THREADS>>>(P, d_gap);
    CK(cudaMemcpy(&gap, d_gap, sizeof(gap), cudaMemcpyDeviceToHost));
    s.gap_stops = 0;
    if (gap < (unsigned long long)K) {
      s.gap_stops = 1;  // certified optimal before eps reached 1
      break;
    }
    eps = std::max(1LL, eps / theta);
  }
  CK(cudaDeviceSynchronize());
  s.solve_s = now() - t1;

  double t2 = now();
  std::vector<int> assign(n);
  CK(cudaMemcpy(assign.data(), P.assign, n * sizeof(int), cudaMemcpyDeviceToHost));
  std::vector<long long> stats(N_STATS);
  CK(cudaMemcpy(stats.data(), P.stats, stats.size() * sizeof(long long), cudaMemcpyDeviceToHost));
  for (int i = 0; i < n; ++i) match[i] = assign[i] >= 0 && assign[i] < m ? assign[i] : -1;
  s.copy_back_s = now() - t2;
  s.rounds = stats[0];
  s.bids = stats[1];
  s.phase_rounds.assign(stats.begin() + 2, stats.begin() + 2 + s.phases);
  s.blocks = blocks;
  s.grid_rounds = stats[STAT_GRID_ROUNDS];
  s.tail_rounds = stats[STAT_TAIL_ROUNDS];
  s.grid_cycles = stats[STAT_GRID_CYCLES];
  s.tail_cycles = stats[STAT_TAIL_CYCLES];
  if (st) *st = s;
  return match;
}
