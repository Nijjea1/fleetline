// Python bindings: fleetline_matcher._cuda_auction.solve(...)
#include <pybind11/numpy.h>
#include <pybind11/pybind11.h>
#include <pybind11/stl.h>

#include <stdexcept>

#include "auction.h"

namespace py = pybind11;
using I32 = py::array_t<int32_t, py::array::c_style | py::array::forcecast>;
using I64 = py::array_t<int64_t, py::array::c_style | py::array::forcecast>;

static py::tuple solve(int n_rows, int n_cols, I32 rows, I32 cols, I64 cost_ms, int64_t unmatched_ms,
                       int theta) {
  auto n = cost_ms.size();
  if (rows.size() != n || cols.size() != n) throw std::invalid_argument("rows, cols, cost_ms differ in length");
  AuctionStats st;
  std::vector<int32_t> m;
  {
    py::gil_scoped_release nogil;
    m = auction_solve(n_rows, n_cols, rows.data(), cols.data(), cost_ms.data(), n, unmatched_ms, theta, &st);
  }
  I32 out(m.size());
  std::copy(m.begin(), m.end(), out.mutable_data());
  py::dict stats;
  stats["setup_s"] = st.setup_s;
  stats["solve_s"] = st.solve_s;
  stats["copy_back_s"] = st.copy_back_s;
  stats["phases"] = st.phases;
  stats["rounds"] = st.rounds;
  stats["bids"] = st.bids;
  stats["phase_rounds"] = st.phase_rounds;
  stats["blocks"] = st.blocks;
  stats["gap_stop"] = st.gap_stops;
  stats["grid_rounds"] = st.grid_rounds;
  stats["tail_rounds"] = st.tail_rounds;
  stats["grid_cycles"] = st.grid_cycles;
  stats["tail_cycles"] = st.tail_cycles;
  return py::make_tuple(out, stats);
}

PYBIND11_MODULE(_cuda_auction, mod) {
  mod.doc() = "Forward auction with epsilon-scaling on the GPU (E4)";
  mod.def("solve", &solve, py::arg("n_rows"), py::arg("n_cols"), py::arg("rows"), py::arg("cols"),
          py::arg("cost_ms"), py::arg("unmatched_ms"), py::arg("theta") = 8);
}
