"use client";
import { clock12 } from "@/lib/format";

/** One line: "main" is solid white, "ref" is the dashed grey comparison line. */
export type Series = { values: number[]; style: "main" | "ref" };

// Median wait per minute for two series. Neutral colors on purpose: status colors belong
// to the cars on the map.
export function WaitChart({ series, t, startMs, label }: { series: Series[]; t: number; startMs: number; label: string }) {
  const W = 320;
  const H = 128;
  const P = { l: 30, r: 10, t: 8, b: 22 };
  const len = Math.max(...series.map((s) => s.values.length));
  const maxMin = Math.ceil((Math.max(60, ...series.flatMap((s) => s.values)) * 1.15) / 60);
  const max = maxMin * 60;
  const last = Math.max(1, len - 1);
  const x = (i: number) => P.l + (i / last) * (W - P.l - P.r);
  const y = (v: number) => H - P.b - (v / max) * (H - P.t - P.b);
  const path = (vs: number[]) => vs.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join("");
  const yTicks = [0, Math.round(maxMin / 2), maxMin].filter((v, i, a) => a.indexOf(v) === i);
  const xTicks = [0, Math.round(last / 2), last];
  const ci = Math.min(t / 60, last);
  const at = Math.min(Math.floor(ci), last);
  // Draw the reference line first so the main line sits on top.
  const ordered = [...series].sort((a, b) => (a.style === b.style ? 0 : a.style === "ref" ? -1 : 1));

  return (
    <figure className="chart-wrap">
      <svg viewBox={`0 0 ${W} ${H}`} className="chart" role="img" aria-label={label}>
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
        {ordered.map((s, k) => (
          <path key={k} d={path(s.values)} className={`line ${s.style === "main" ? "sim" : "real"}`} />
        ))}
        <line x1={x(ci)} x2={x(ci)} y1={P.t} y2={H - P.b} className="now" />
        {ordered.map((s, k) => (
          <circle key={k} cx={x(ci)} cy={y(s.values[at] ?? 0)} r={3} className={`pt ${s.style === "main" ? "sim" : "real"}`} />
        ))}
      </svg>
    </figure>
  );
}
