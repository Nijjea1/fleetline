// E4 GPU solver: forward auction with epsilon-scaling (Bertsekas) for the sparse
// rider -> car assignment. See matcher/README.md and docs/learning/10-gpu-auction.md.
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
// Exactness: costs are whole milliseconds, multiplied by K = N + 1. Auction ends within
// N * eps of the optimum, so the last phase at eps = 1 is off by less than K, i.e. less
// than one millisecond: it is the exact optimum of the millisecond-rounded problem.
#include "auction.h"

#include <cuda_runtime.h>

#include <algorithm>
#include <chrono>
#include <climits>
#include <cstdio>
#include <stdexcept>
#include <string>

namespace {

constexpr long long NEG = LLONG_MIN / 4;

#define CK(x)                                                                              \
  do {                                                                                     \
    cudaError_t e_ = (x);                                                                  \
    if (e_ != cudaSuccess)                                                                 \
      throw std::runtime_error(std::string(#x) + ": " + cudaGetErrorString(e_));           \
  } while (0)

double now() {
  return std::chrono::duration<double>(std::chrono::steady_clock::now().time_since_epoch()).count();
}

struct Dev {
  int *off = nullptr, *obj = nullptr;  // CSR by person
  long long* ben = nullptr;            // benefit per edge (scaled, <= 0)
  long long* price = nullptr;          // per object
  int* owner = nullptr;                // object -> person or -1
  int* assign = nullptr;               // person -> object or -1
  long long* bid_val = nullptr;        // person's current bid
  int* bid_obj = nullptr;              // object the person bids on
  long long* obj_best = nullptr;       // highest bid this round per object
  int* obj_win = nullptr;              // lowest person id among the highest bidders
  int* wl = nullptr;                   // two worklists of unassigned persons, N each
  int* cnt = nullptr;                  // their two lengths
  ~Dev() {
    for (void* p : {(void*)off, (void*)obj, (void*)ben, (void*)price, (void*)owner, (void*)assign,
                    (void*)bid_val, (void*)bid_obj, (void*)obj_best, (void*)obj_win, (void*)wl, (void*)cnt})
      if (p) cudaFree(p);
  }
};

// Phase start: everyone unassigned, all persons on worklist 0. Prices carry over.
__global__ void init_phase(int N, int* owner, int* assign, int* wl0, int* cnt, long long* obj_best, int* obj_win) {
  for (int t = blockIdx.x * blockDim.x + threadIdx.x; t < N; t += gridDim.x * blockDim.x) {
    owner[t] = -1;
    assign[t] = -1;
    wl0[t] = t;
    obj_best[t] = 0;
    obj_win[t] = INT_MAX;
  }
  if (blockIdx.x == 0 && threadIdx.x == 0) {
    cnt[0] = N;
    cnt[1] = 0;
  }
}

// Every unassigned person finds its best and second-best object (value = benefit - price)
// and bids the price that makes it indifferent between them, plus eps.
__global__ void bid(const int* wl_in, const int* cnt_in, int* cnt_out, const int* off, const int* obj,
                    const long long* ben, const long long* price, long long* bid_val, int* bid_obj,
                    long long* obj_best, long long eps, long long lone_incr) {
  int n = *cnt_in;
  if (blockIdx.x == 0 && threadIdx.x == 0) *cnt_out = 0;  // last read by the previous round's award
  for (int t = blockIdx.x * blockDim.x + threadIdx.x; t < n; t += gridDim.x * blockDim.x) {
    int i = wl_in[t];
    long long v1 = NEG, v2 = NEG;
    int j1 = -1;
    for (int e = off[i]; e < off[i + 1]; ++e) {
      int j = obj[e];
      long long v = ben[e] - price[j];
      if (v > v1) {
        v2 = v1;
        v1 = v;
        j1 = j;
      } else if (v > v2) {
        v2 = v;
      }
    }
    long long b = price[j1] + (v2 == NEG ? lone_incr : v1 - v2) + eps;
    bid_val[i] = b;
    bid_obj[i] = j1;
    atomicMax(&obj_best[j1], b);
  }
}

// Ties on the highest bid go to the lowest person id (deterministic).
__global__ void resolve(const int* wl_in, const int* cnt_in, const long long* bid_val, const int* bid_obj,
                        const long long* obj_best, int* obj_win) {
  int n = *cnt_in;
  for (int t = blockIdx.x * blockDim.x + threadIdx.x; t < n; t += gridDim.x * blockDim.x) {
    int i = wl_in[t];
    int j = bid_obj[i];
    if (bid_val[i] == obj_best[j]) atomicMin(&obj_win[j], i);
  }
}

// Winners take their object at the bid price; the evicted owner and the losers go on the
// next worklist. Only the (unique) winner touches its object, so it also resets it.
__global__ void award(const int* wl_in, const int* cnt_in, int* wl_out, int* cnt_out, const long long* bid_val,
                      const int* bid_obj, long long* obj_best, int* obj_win, long long* price, int* owner,
                      int* assign) {
  int n = *cnt_in;
  for (int t = blockIdx.x * blockDim.x + threadIdx.x; t < n; t += gridDim.x * blockDim.x) {
    int i = wl_in[t];
    int j = bid_obj[i];
    if (obj_win[j] == i) {
      int old = owner[j];
      owner[j] = i;
      assign[i] = j;
      price[j] = bid_val[i];
      obj_best[j] = 0;
      obj_win[j] = INT_MAX;
      if (old >= 0) {
        assign[old] = -1;
        wl_out[atomicAdd(cnt_out, 1)] = old;
      }
    } else {
      wl_out[atomicAdd(cnt_out, 1)] = i;
    }
  }
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

}  // namespace

std::vector<int32_t> auction_solve(int n_rows, int n_cols, const int32_t* rows, const int32_t* cols,
                                   const int64_t* cost_ms, int64_t n_edges, int64_t unmatched_ms, int theta,
                                   int check_every, AuctionStats* st) {
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
  std::vector<int> deg(N, 0);
  for (int64_t e = 0; e < n_edges; ++e) {
    deg[rows[e]]++;
    deg[n + cols[e]]++;
  }
  std::vector<int> off(N + 1, 0);
  for (int p = 0; p < N; ++p) off[p + 1] = off[p] + deg[p] + 1;  // +1: dummy or own car
  std::vector<int> obj(off[N]);
  std::vector<long long> ben(off[N]);
  std::vector<int> pos(off.begin(), off.end() - 1);
  long long cmax = unmatched_ms * K;
  for (int64_t e = 0; e < n_edges; ++e) {
    int i = rows[e], j = cols[e];
    obj[pos[i]] = j;
    ben[pos[i]++] = -cost_ms[e] * K;
    obj[pos[n + j]] = m + i;
    ben[pos[n + j]++] = 0;
    cmax = std::max(cmax, (long long)cost_ms[e] * K);
  }
  for (int i = 0; i < n; ++i) {
    obj[pos[i]] = m + i;
    ben[pos[i]++] = -unmatched_ms * K;
  }
  for (int j = 0; j < m; ++j) {
    obj[pos[n + j]] = j;
    ben[pos[n + j]++] = 0;
  }

  Dev d;
  d.off = upload(off);
  d.obj = upload(obj);
  d.ben = upload(ben);
  d.price = alloc<long long>(N);
  CK(cudaMemset(d.price, 0, N * sizeof(long long)));
  d.owner = alloc<int>(N);
  d.assign = alloc<int>(N);
  d.bid_val = alloc<long long>(N);
  d.bid_obj = alloc<int>(N);
  d.obj_best = alloc<long long>(N);
  d.obj_win = alloc<int>(N);
  d.wl = alloc<int>(2 * (size_t)N);
  d.cnt = alloc<int>(2);
  CK(cudaDeviceSynchronize());
  s.setup_s = now() - t0;

  int dev, sms;
  CK(cudaGetDevice(&dev));
  CK(cudaDeviceGetAttribute(&sms, cudaDevAttrMultiProcessorCount, dev));
  const int threads = 256;
  const int blocks = std::max(1, std::min((N + threads - 1) / threads, sms * 8));

  double t1 = now();
  long long eps = std::max(1LL, cmax / theta);
  int h_cnt = 0;
  while (true) {
    init_phase<<<blocks, threads>>>(N, d.owner, d.assign, d.wl, d.cnt, d.obj_best, d.obj_win);
    for (long long r = 0;; ++r) {
      int in = r & 1, out = in ^ 1;
      int* wl_in = d.wl + (size_t)in * N;
      int* wl_out = d.wl + (size_t)out * N;
      bid<<<blocks, threads>>>(wl_in, d.cnt + in, d.cnt + out, d.off, d.obj, d.ben, d.price, d.bid_val, d.bid_obj,
                               d.obj_best, eps, cmax);
      resolve<<<blocks, threads>>>(wl_in, d.cnt + in, d.bid_val, d.bid_obj, d.obj_best, d.obj_win);
      award<<<blocks, threads>>>(wl_in, d.cnt + in, wl_out, d.cnt + out, d.bid_val, d.bid_obj, d.obj_best, d.obj_win,
                                 d.price, d.owner, d.assign);
      s.rounds++;
      // Reading the worklist length forces a host sync, so only check every few rounds;
      // rounds on an empty worklist do nothing.
      if ((r + 1) % check_every == 0) {
        CK(cudaMemcpy(&h_cnt, d.cnt + out, sizeof(int), cudaMemcpyDeviceToHost));
        if (h_cnt == 0) break;
      }
    }
    s.phases++;
    if (eps == 1) break;
    eps = std::max(1LL, eps / theta);
  }
  CK(cudaGetLastError());
  CK(cudaDeviceSynchronize());
  s.solve_s = now() - t1;

  double t2 = now();
  std::vector<int> assign(n);
  CK(cudaMemcpy(assign.data(), d.assign, n * sizeof(int), cudaMemcpyDeviceToHost));
  for (int i = 0; i < n; ++i) match[i] = assign[i] < m ? assign[i] : -1;
  s.copy_back_s = now() - t2;
  if (st) *st = s;
  return match;
}
