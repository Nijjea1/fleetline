# Optional GPU solver for E4. Build with matcher/cuda/build.sh (needs nvcc; CI skips it).
cmake_minimum_required(VERSION 3.24)
# Compile for the GPU in this machine (sm_120 on the RTX 5060). Must come before project(),
# which otherwise fills in an old default (sm_52).
if(NOT DEFINED CMAKE_CUDA_ARCHITECTURES)
  set(CMAKE_CUDA_ARCHITECTURES native)
endif()
project(fleetline_cuda LANGUAGES CXX CUDA)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CUDA_STANDARD 17)

find_package(Python COMPONENTS Interpreter Development.Module REQUIRED)
find_package(pybind11 CONFIG REQUIRED)

pybind11_add_module(_cuda_auction auction_py.cpp auction.cu)
target_compile_options(_cuda_auction PRIVATE $<$<COMPILE_LANGUAGE:CUDA>:-lineinfo>)
# Drop the module straight into the package so `from . import _cuda_auction` finds it.
set_target_properties(_cuda_auction PROPERTIES LIBRARY_OUTPUT_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}/../src/fleetline_matcher)
