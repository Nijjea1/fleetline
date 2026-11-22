# av/: robotaxi motion model (stretch, Phase 4, E8)

Not started. Blocked on dataset access: the Waymo Open Motion Dataset requires accepting a
non-commercial license at https://waymo.com/open and downloading with `gsutil`.

Plan when unblocked:
1. Constant-velocity baseline (the number to beat).
2. Small vectorized transformer predicting K future paths per agent, sized for 8 GB VRAM
   (data subset, mixed precision).
3. Export to ONNX -> TensorRT like the ETA model.
4. In Fleetline: flag robotaxi pickups whose stopping point conflicts with predicted traffic.
