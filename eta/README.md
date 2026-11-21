# eta/

ETA model. Phase 3.

OSRM road time as the baseline, a small PyTorch MLP predicting the *residual* (actual − OSRM), exported to ONNX → TensorRT and served from C++.
