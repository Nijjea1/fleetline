// Loads and decodes a recorded run written by core/cmd/export-replay.
// Layout (little-endian), see export-replay for the authoritative description:
//   frames.bin : per frame  lat[N]u16 lon[N]u16 status[N]u8 nPending:u16 (lat,lon u16)[nPending]
//   pickups.bin: per pickup t:u16 lat:u16 lon:u16 wait:u16 kind:u8   (9 bytes, packed)

export type Minute = {
  t: number;
  online: number;
  busy: number;
  pending: number;
  pickups: number;
  sim_p50_s: number;
  real_p50_s: number;
  requests: number;
  unserved: number;
};

export type Meta = {
  day: string;
  from: string;
  to: string;
  start_ms: number;
  frame_s: number;
  frames: number;
  vehicles: number;
  kinds: string; // one char per vehicle: 'h' human, 'a' robotaxi
  bbox: [number, number, number, number]; // minLon, minLat, maxLon, maxLat
  robotaxi_zone: [number, number][];
  pickups: number;
  minutes: Minute[];
  params: Record<string, string>;
};

export type Frame = {
  lat: Uint16Array;
  lon: Uint16Array;
  status: Uint8Array; // 0 offline, 1 idle, 2 en route, 3 on trip
  pending: Uint16Array; // lat, lon pairs
};

export type Pickup = { t: number; lon: number; lat: number; wait: number; av: boolean };

export type Replay = {
  meta: Meta;
  frames: Frame[];
  pickups: Pickup[];
  isAV: Uint8Array;
  bytes: number;
};

export async function loadReplay(base = "replay"): Promise<Replay> {
  const [meta, fb, pb] = await Promise.all([
    fetch(`${base}/meta.json`).then((r) => r.json() as Promise<Meta>),
    fetch(`${base}/frames.bin`).then((r) => r.arrayBuffer()),
    fetch(`${base}/pickups.bin`).then((r) => r.arrayBuffer()),
  ]);
  const n = meta.vehicles;
  const frames: Frame[] = [];
  const dv = new DataView(fb);
  let off = 0;
  // Copy each block out: blocks after the 1-byte status column aren't 2-byte aligned,
  // so a Uint16Array view straight onto the buffer would throw.
  const u16 = (count: number) => {
    const a = new Uint16Array(count);
    for (let i = 0; i < count; i++) a[i] = dv.getUint16(off + 2 * i, true);
    off += 2 * count;
    return a;
  };
  for (let f = 0; f < meta.frames; f++) {
    const lat = u16(n);
    const lon = u16(n);
    const status = new Uint8Array(fb.slice(off, off + n));
    off += n;
    const np = dv.getUint16(off, true);
    off += 2;
    frames.push({ lat, lon, status, pending: u16(2 * np) });
  }
  const [minLon, minLat, maxLon, maxLat] = meta.bbox;
  const pdv = new DataView(pb);
  const pickups: Pickup[] = [];
  for (let o = 0; o + 9 <= pb.byteLength; o += 9) {
    pickups.push({
      t: pdv.getUint16(o, true),
      lat: minLat + (pdv.getUint16(o + 2, true) / 65535) * (maxLat - minLat),
      lon: minLon + (pdv.getUint16(o + 4, true) / 65535) * (maxLon - minLon),
      wait: pdv.getUint16(o + 6, true),
      av: pdv.getUint8(o + 8) === 1,
    });
  }
  const isAV = new Uint8Array(n);
  for (let i = 0; i < n; i++) isAV[i] = meta.kinds.charCodeAt(i) === 97 ? 1 : 0; // 'a'
  return { meta, frames, pickups, isAV, bytes: fb.byteLength + pb.byteLength };
}

export const STATUS_COLORS: Record<number, [number, number, number]> = {
  1: [110, 168, 254], // idle: blue
  2: [255, 180, 84], // en route to pickup: amber
  3: [61, 220, 151], // on trip: green
};

/** Positions and colors for every vehicle at time t (s since window start). */
export function vehiclesAt(r: Replay, t: number, out: { pos: Float32Array; col: Uint8Array; counts: number[] }) {
  const { meta, frames } = r;
  const [minLon, minLat, maxLon, maxLat] = meta.bbox;
  const sLat = (maxLat - minLat) / 65535;
  const sLon = (maxLon - minLon) / 65535;
  const x = Math.max(0, Math.min(t / meta.frame_s, frames.length - 1));
  const f0 = Math.floor(x);
  const f1 = Math.min(f0 + 1, frames.length - 1);
  const a = x - f0;
  const A = frames[f0];
  const B = frames[f1];
  const counts = [0, 0, 0, 0];
  for (let i = 0; i < meta.vehicles; i++) {
    const s = A.status[i];
    counts[s]++;
    // A car that comes online in the next frame shouldn't slide in from (0,0).
    const b = B.status[i] === 0 || s === 0 ? 0 : a;
    out.pos[2 * i] = minLon + (A.lon[i] + (B.lon[i] - A.lon[i]) * b) * sLon;
    out.pos[2 * i + 1] = minLat + (A.lat[i] + (B.lat[i] - A.lat[i]) * b) * sLat;
    const c = STATUS_COLORS[s];
    if (c) {
      out.col[4 * i] = c[0];
      out.col[4 * i + 1] = c[1];
      out.col[4 * i + 2] = c[2];
      out.col[4 * i + 3] = 220;
    } else {
      out.col[4 * i + 3] = 0;
    }
  }
  out.counts = counts;
}

export function pendingAt(r: Replay, t: number): Float32Array {
  const { meta, frames } = r;
  const f = frames[Math.max(0, Math.min(Math.round(t / meta.frame_s), frames.length - 1))];
  const [minLon, minLat, maxLon, maxLat] = meta.bbox;
  const out = new Float32Array(f.pending.length);
  for (let i = 0; i < f.pending.length; i += 2) {
    out[i] = minLon + (f.pending[i + 1] / 65535) * (maxLon - minLon);
    out[i + 1] = minLat + (f.pending[i] / 65535) * (maxLat - minLat);
  }
  return out;
}
