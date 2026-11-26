"use client";
// Greedy vs batched on the same recorded morning: same riders, same fleet, same city,
// two maps on one camera and one clock. Only the matching rule differs (E2, D-016).
import type { MapViewState } from "@deck.gl/core";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { clock12, longDay, mmss, n } from "@/lib/format";
import { loadReplay, type Replay } from "@/lib/replay";
import { STATUS, css } from "@/lib/theme";
import { Notice } from "./Notice";
import { fitNYC, MapView } from "./MapView";
import { MapKey, Section } from "./Panel";
import { Timeline } from "./Timeline";
import { WaitChart } from "./WaitChart";
import { replayLayers, replayTooltip, useClock, useFrame, type FrameData } from "./replayLayers";

const POLICIES = {
  greedy: { name: "Greedy", how: "Each rider gets the nearest free car, one at a time" },
  batched: { name: "Batched", how: "Every second, all waiting riders are matched together" },
} as const;

/** Pickups up to time t (sorted once, then binary search). */
function usePickupTally(r: Replay | null) {
  return useMemo(() => {
    if (!r) return () => 0;
    const times = r.pickups.map((p) => p.t).sort((a, b) => a - b);
    return (t: number) => {
      let lo = 0;
      let hi = times.length;
      while (lo < hi) {
        const mid = (lo + hi) >> 1;
        if (times[mid] <= t) lo = mid + 1;
        else hi = mid;
      }
      return lo;
    };
  }, [r]);
}

function startView(): MapViewState {
  const w = window.innerWidth;
  const h = window.innerHeight;
  if (w > 900) {
    const paneW = (w - 360 - 32) / 2;
    return fitNYC(paneW, h, { left: 16, right: 16, top: 120, bottom: 100 });
  }
  return fitNYC(w / 2, h * 0.5, { left: 6, right: 6, top: 70, bottom: 6 });
}

const pctChange = (b: number, a: number) => (a > 0 ? Math.round((100 * (b - a)) / a) : 0);

export function CompareView({ brand }: { brand: ReactNode }) {
  const [runs, setRuns] = useState<{ greedy: Replay; batched: Replay } | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [view, setView] = useState<MapViewState>(startView);

  useEffect(() => {
    Promise.all([loadReplay("replay"), loadReplay("replay-batched")]).then(
      ([greedy, batched]) => setRuns({ greedy, batched }),
      (e) => setErr(String(e)),
    );
  }, []);

  const g = runs?.greedy ?? null;
  const b = runs?.batched ?? null;
  const duration = g ? (g.meta.frames - 1) * g.meta.frame_s : 0;
  const { t, setT, playing, setPlaying, speed, setSpeed } = useClock(duration);
  const fg = useFrame(g, t);
  const fb = useFrame(b, t);
  const tallyG = usePickupTally(g);
  const tallyB = usePickupTally(b);

  if (err) return <Notice title="Couldn't load the recordings" body={err} />;
  if (!g || !b || !fg || !fb) return <Notice title="Loading two recorded runs of the same morning…" body="Greedy and batched · about 12 MB" spinner />;

  const meta = g.meta;
  const endMs = meta.start_ms + meta.frames * meta.frame_s * 1000;
  const sg = g.meta.summary;
  const sb = b.meta.summary;
  const now = { greedy: tallyG(t), batched: tallyB(t) };
  const meanDelta = pctChange(sb.sim_wait.mean_s, sg.sim_wait.mean_s);

  return (
    <>
      <div className="compare">
        <Pane policy="greedy" replay={g} frame={fg} t={t} view={view} onView={setView} picked={now.greedy} />
        <Pane policy="batched" replay={b} frame={fb} t={t} view={view} onView={setView} picked={now.batched} chrome />
      </div>
      <aside className="panel">
        {brand}
        <p className="context">
          {longDay(meta.day)} · {clock12(meta.start_ms, false)}–{clock12(endMs, false)} · {n(sg.requests)} real requests
        </p>

        <Section title="Same riders, same cars">
          <p className="body">
            Both maps replay the same morning with the same fleet. The only difference is how cars are matched to riders.
            Watch the <b style={{ color: css(STATUS.pickup.rgb) }}>amber</b> cars driving empty to pickups and the{" "}
            <b className="rider-word">red</b> riders still waiting.
          </p>
        </Section>

        <Section title="Average wait, these 20 minutes">
          <div className="kpis">
            <div className="kpi">
              <span className="kpi-val dim">{mmss(sg.sim_wait.mean_s)}</span>
              <span className="kpi-lbl">
                <i className="lg real" /> Greedy
              </span>
            </div>
            <div className="kpi">
              <span className="kpi-val">{mmss(sb.sim_wait.mean_s)}</span>
              <span className="kpi-lbl">
                <i className="lg sim" /> Batched <span className="delta good">{meanDelta}%</span>
              </span>
            </div>
          </div>
          <table className="vs">
            <thead>
              <tr>
                <th />
                <th>Greedy</th>
                <th>Batched</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>Slowest 10% wait over</td>
                <td>{mmss(sg.sim_wait.p90_s)}</td>
                <td>{mmss(sb.sim_wait.p90_s)}</td>
              </tr>
              <tr>
                <td>Empty drive to each pickup</td>
                <td>{mmss(sg.pickup_eta.mean_s)}</td>
                <td>{mmss(sb.pickup_eta.mean_s)}</td>
              </tr>
            </tbody>
          </table>
          <WaitChart
            series={[
              { values: g.meta.minutes.map((m) => m.sim_p50_s), style: "ref" },
              { values: b.meta.minutes.map((m) => m.sim_p50_s), style: "main" },
            ]}
            t={t}
            startMs={meta.start_ms}
            label="Median rider wait per minute, greedy vs batched"
          />
          <p className="note">
            Median wait by request minute. Over a full held-out day (Sep 23, 603k requests, 3 seeds) batched cut the mean
            wait 18.6% and the 90th percentile 15.7%.
          </p>
        </Section>

        <Section title="So far" aside={clock12(meta.start_ms + t * 1000)}>
          <table className="vs">
            <thead>
              <tr>
                <th />
                <th>Greedy</th>
                <th>Batched</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>Riders picked up</td>
                <td>{n(now.greedy)}</td>
                <td>{n(now.batched)}</td>
              </tr>
              <tr>
                <td>Riders waiting now</td>
                <td>{n(fg.pending.length / 2)}</td>
                <td>{n(fb.pending.length / 2)}</td>
              </tr>
              <tr>
                <td>Cars driving to a pickup</td>
                <td>{n(fg.counts[2])}</td>
                <td>{n(fb.counts[2])}</td>
              </tr>
            </tbody>
          </table>
        </Section>

        <MapKey replay />
      </aside>
      <Timeline
        t={t}
        duration={duration}
        startMs={meta.start_ms}
        endMs={endMs}
        day={longDay(meta.day)}
        playing={playing}
        speed={speed}
        onPlaying={setPlaying}
        onSeek={setT}
        onSpeed={setSpeed}
      />
    </>
  );
}

function Pane(p: {
  policy: keyof typeof POLICIES;
  replay: Replay;
  frame: FrameData;
  t: number;
  view: MapViewState;
  onView: (v: MapViewState) => void;
  picked: number;
  chrome?: boolean;
}) {
  const { name, how } = POLICIES[p.policy];
  const tooltip = useMemo(() => replayTooltip(p.replay, p.t), [p.replay, p.t]);
  return (
    <section className="pane" aria-label={`${name} matching`}>
      <MapView layers={replayLayers(p.replay, p.frame, p.t)} tooltip={tooltip} view={p.view} onView={p.onView} chrome={!!p.chrome} />
      <header className={`pane-head ${p.policy}`}>
        <div>
          <h2>{name}</h2>
          <p>{how}</p>
        </div>
        <dl>
          <div>
            <dt>Waiting now</dt>
            <dd className="rider-num">{n(p.frame.pending.length / 2)}</dd>
          </div>
          <div>
            <dt>Picked up</dt>
            <dd>{n(p.picked)}</dd>
          </div>
        </dl>
      </header>
    </section>
  );
}
