"use client";
import type { Minute } from "@/lib/replay";

// Median wait per minute: simulated greedy dispatch vs what Uber/Lyft riders actually
// waited for the same requests. Plain SVG; nothing here needs a chart library.
export function WaitChart({ minutes, t }: { minutes: Minute[]; t: number }) {
  const W = 300;
  const H = 110;
  const P = { l: 34, r: 8, t: 10, b: 20 };
  const max = Math.max(60, ...minutes.flatMap((m) => [m.sim_p50_s, m.real_p50_s])) * 1.1;
  const x = (i: number) => P.l + (i / Math.max(1, minutes.length - 1)) * (W - P.l - P.r);
  const y = (v: number) => H - P.b - (v / max) * (H - P.t - P.b);
  const path = (k: "sim_p50_s" | "real_p50_s") =>
    minutes.map((m, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(m[k]).toFixed(1)}`).join("");
  const ticks = [0, Math.round(max / 2 / 60) * 60, Math.floor(max / 60) * 60].filter((v, i, a) => a.indexOf(v) === i);
  const cur = x(Math.min(t / 60, minutes.length - 1));
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="chart" role="img" aria-label="Median wait per minute, simulated vs real">
      {ticks.map((v) => (
        <g key={v}>
          <line x1={P.l} x2={W - P.r} y1={y(v)} y2={y(v)} className="grid" />
          <text x={P.l - 6} y={y(v) + 3} className="tick" textAnchor="end">
            {v / 60}m
          </text>
        </g>
      ))}
      <path d={path("real_p50_s")} className="line real" />
      <path d={path("sim_p50_s")} className="line sim" />
      <line x1={cur} x2={cur} y1={P.t} y2={H - P.b} className="now" />
      <text x={P.l} y={H - 4} className="tick">
        {minutes.length} min window
      </text>
    </svg>
  );
}
