// F1: smallest possible proof that the CUDA toolchain and the RTX 5060 work.
// Build + run: nvcc -O2 -o vecadd vecadd.cu && ./vecadd
#include <cstdio>
#include <cmath>
#include <vector>
#include <cuda_runtime.h>

#define CHECK(x) do { cudaError_t e = (x); if (e != cudaSuccess) { \
  std::printf("CUDA error %s at %s:%d\n", cudaGetErrorString(e), __FILE__, __LINE__); return 1; } } while (0)

__global__ void add(const float* a, const float* b, float* c, int n) {
  int i = blockIdx.x * blockDim.x + threadIdx.x;  // one thread per element
  if (i < n) c[i] = a[i] + b[i];
}

int main() {
  cudaDeviceProp p;
  CHECK(cudaGetDeviceProperties(&p, 0));
  std::printf("GPU: %s, compute %d.%d, %d SMs, %.1f GB\n", p.name, p.major, p.minor,
              p.multiProcessorCount, p.totalGlobalMem / 1e9);

  const int n = 1 << 24;  // 16M floats
  std::vector<float> a(n), b(n), c(n);
  for (int i = 0; i < n; i++) { a[i] = i * 0.5f; b[i] = 2.0f * i; }
  float *da, *db, *dc;
  CHECK(cudaMalloc(&da, n * sizeof(float)));
  CHECK(cudaMalloc(&db, n * sizeof(float)));
  CHECK(cudaMalloc(&dc, n * sizeof(float)));
  CHECK(cudaMemcpy(da, a.data(), n * sizeof(float), cudaMemcpyHostToDevice));
  CHECK(cudaMemcpy(db, b.data(), n * sizeof(float), cudaMemcpyHostToDevice));

  cudaEvent_t t0, t1;
  cudaEventCreate(&t0); cudaEventCreate(&t1);
  add<<<(n + 255) / 256, 256>>>(da, db, dc, n);  // warm-up
  cudaEventRecord(t0);
  add<<<(n + 255) / 256, 256>>>(da, db, dc, n);
  cudaEventRecord(t1);
  CHECK(cudaEventSynchronize(t1));
  float ms = 0; cudaEventElapsedTime(&ms, t0, t1);
  CHECK(cudaMemcpy(c.data(), dc, n * sizeof(float), cudaMemcpyDeviceToHost));

  int bad = 0;
  for (int i = 0; i < n; i++) if (std::fabs(c[i] - 2.5f * i) > 1e-3f * (1 + i)) bad++;
  // 3 arrays x 4 bytes x n moved by the kernel
  std::printf("vector add of %d floats: %.3f ms, %.0f GB/s, %s\n", n, ms,
              3.0 * 4 * n / (ms / 1e3) / 1e9, bad ? "WRONG" : "correct");
  cudaFree(da); cudaFree(db); cudaFree(dc);
  return bad ? 1 : 0;
}
