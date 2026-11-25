"use client";
import { clock12 } from "@/lib/format";
import type { Minute } from "@/lib/replay";

// Median wait per minute: Fleetline's dispatch vs what riders actually waited for the same
// requests. Neutral colors on purpose: status colors belong to the cars on the map.
export function WaitChart({ minutes, t, startMs }: { minutes: Minute[]; t: number; startMs: number }) {
  const W = 320;
  const H = 128;
  const P = { l: 30, r: 10, t: 8, b: 22 };
  const maxMin = Math.ceil((Math.max(60, ...minutes.flatMap((m) => [m.sim_p50_s, m.real_p50_s])) * 1.15) / 60);
  const max = maxMin * 60;
  const last = Math.max(1, minutes.length - 1);
  const x = (i: number) => P.l + (i / last) * (W - P.l - P.r);
  const y = (v: number) => H - P.b - (v / max) * (H - P.t - P.b);
  const path = (k: "sim_p50_s" | "real_p50_s") =>
    minutes.map((m, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(m[k]).toFixed(1)}`).join("");
  const yTicks = [0, Math.round(maxMin / 2), maxMin].filter((v, i, a) => a.indexOf(v) === i);
  const xTicks = [0, Math.round(last / 2), last];
  const ci = Math.min(t / 60, last);
  const at = minutes[Math.min(Math.floor(ci), last)];

  return (
    <figure className="chart-wrap">
      <svg viewBox={`0 0 ${W} ${H}`} className="chart" role="img" aria-label="Median rider wait per minute, Fleetline vs actual">
        {yTicks.map((v) => (
          <g key={v}>
            <line x1={P.l} x2={W - P.r} y1={y(v * 60)} y2={y(v * 60)} className="grid" />
            <text x={P.l - 6} y={y(v * 60) + 3} className="tick" textAnchor="end">
              {v} min
            </text>
          </g>
        ))}
        {xTicks.map((i) => (
          <text key={i} x={x(i)} y={H - 6} className="tick" textAnchor={i === 0 ? "start" : i === last ? "end" : "middle"}>
            {clock12(startMs + i * 60_000, false)}
          </text>
        ))}
        <path d={path("real_p50_s")} className="line real" />
        <path d={path("sim_p50_s")} className="line sim" />
        <line x1={x(ci)} x2={x(ci)} y1={P.t} y2={H - P.b} className="now" />
        <circle cx={x(ci)} cy={y(at.real_p50_s)} r={3} className="pt real" />
        <circle cx={x(ci)} cy={y(at.sim_p50_s)} r={3} className="pt sim" />
      </svg>
    </figure>
  );
}
