"use client";
// Map layers and hover text for a recorded run. Shared by the single replay and the
// side-by-side compare view, so both draw cars, riders and pickups identically.
import type { Layer, PickingInfo } from "@deck.gl/core";
import { PolygonLayer, ScatterplotLayer } from "@deck.gl/layers";
import { useEffect, useMemo, useState } from "react";
import { mmss } from "@/lib/format";
import { pendingAt, vehiclesAt, type Pickup, type Replay } from "@/lib/replay";
import { RIDER, ROBOTAXI, STATUS_BY_CODE } from "@/lib/theme";

export const PICKUP_FADE_S = 25;

// Dots scale with zoom (meters), clamped so they stay visible zoomed out and don't turn
// into blobs zoomed in.
export const CAR_STYLE = { radiusUnits: "meters", getRadius: 22, radiusMinPixels: 1.3, radiusMaxPixels: 6 } as const;
export const RIDER_STYLE = {
  radiusUnits: "meters",
  getRadius: 34,
  radiusMinPixels: 2,
  radiusMaxPixels: 8,
  getFillColor: [...RIDER, 240] as [number, number, number, number],
} as const;

export function carLabel(status: number, av: boolean) {
  return `${av ? "Robotaxi" : "Human driver"} · ${STATUS_BY_CODE[status]?.label ?? "Offline"}`;
}

export type FrameData = { pos: Float32Array; col: Uint8Array; counts: number[]; pending: Float32Array };

/** Car positions/colors and waiting riders at time t (s since the window start). */
export function useFrame(replay: Replay | null, t: number): FrameData | null {
  return useMemo(() => {
    if (!replay) return null;
    const count = replay.meta.vehicles;
    // Fresh arrays each frame so deck.gl sees new attribute data.
    const out = { pos: new Float32Array(2 * count), col: new Uint8Array(4 * count), counts: [0, 0, 0, 0] };
    vehiclesAt(replay, t, out);
    return { ...out, pending: pendingAt(replay, t) };
  }, [replay, t]);
}

/** Simulated playback clock: `speed` simulated seconds per wall second, looping. */
export function useClock(duration: number) {
  const [t, setT] = useState(0);
  const [playing, setPlaying] = useState(true);
  const [speed, setSpeed] = useState(30);
  useEffect(() => {
    if (!duration || !playing) return;
    let raf = 0;
    let last = performance.now();
    const tick = (now: number) => {
      const dt = Math.min(0.1, (now - last) / 1000); // no jump after a background tab
      last = now;
      setT((prev) => (prev + dt * speed) % duration);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [playing, speed, duration]);
  return { t, setT, playing, setPlaying, speed, setSpeed };
}

export function replayLayers(replay: Replay, frame: FrameData, t: number): Layer[] {
  const { meta } = replay;
  const recent = replay.pickups.filter((p) => p.t <= t && t - p.t < PICKUP_FADE_S);
  return [
    new PolygonLayer({
      id: "zone",
      data: [{ polygon: meta.robotaxi_zone }],
      getPolygon: (d: { polygon: [number, number][] }) => d.polygon,
      getFillColor: [...ROBOTAXI, 18],
      getLineColor: [...ROBOTAXI, 170],
      lineWidthMinPixels: 1.5,
    }),
    new ScatterplotLayer({
      id: "vehicles",
      data: { length: meta.vehicles, attributes: { getPosition: { value: frame.pos, size: 2 }, getFillColor: { value: frame.col, size: 4 } } },
      ...CAR_STYLE,
      pickable: true,
    }),
    new ScatterplotLayer({
      id: "pending",
      data: { length: frame.pending.length / 2, attributes: { getPosition: { value: frame.pending, size: 2 } } },
      ...RIDER_STYLE,
      pickable: true,
    }),
    new ScatterplotLayer({
      id: "pickups",
      data: recent,
      getPosition: (p: Pickup) => [p.lon, p.lat],
      radiusUnits: "pixels",
      getRadius: (p: Pickup) => 3 + ((t - p.t) / PICKUP_FADE_S) * 12,
      stroked: true,
      filled: false,
      getLineColor: (p: Pickup) =>
        [...(p.av ? ROBOTAXI : [255, 255, 255]), Math.round(170 * (1 - (t - p.t) / PICKUP_FADE_S))] as [number, number, number, number],
      lineWidthMinPixels: 1,
      pickable: true,
      updateTriggers: { getRadius: t, getLineColor: t },
    }),
  ];
}

export function replayTooltip(replay: Replay, t: number) {
  return (info: PickingInfo) => {
    const id = info.layer?.id;
    if (id === "vehicles") {
      const f = replay.frames[Math.min(Math.floor(t / replay.meta.frame_s), replay.frames.length - 1)];
      return carLabel(f.status[info.index], replay.isAV[info.index] === 1);
    }
    if (id === "pending") return "Rider waiting for a car";
    if (id === "pickups") {
      const p = info.object as Pickup;
      return `Picked up by a ${p.av ? "robotaxi" : "driver"} after a ${mmss(p.wait)} wait`;
    }
    return null;
  };
}
