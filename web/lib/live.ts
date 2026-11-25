// Live mode: poll fleetd's /live endpoint (see core/cmd/fleetd). Run `make demo` first.
export type LiveSnapshot = {
  sim_time: string;
  vehicles: number[]; // flat [lat, lon, status, kind, ...]
  pending: number[]; // flat [lat, lon, ...]
  served: number;
  expired: number;
  unserved_rate: number;
  wait_p50_s: number;
  wait_p90_s: number;
};

export async function fetchLive(base: string, signal?: AbortSignal): Promise<LiveSnapshot> {
  const r = await fetch(`${base}/live`, { signal, cache: "no-store" });
  if (!r.ok) throw new Error(`fleetd ${r.status}`);
  return r.json();
}
