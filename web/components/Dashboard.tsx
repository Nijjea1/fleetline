"use client";
import DeckGL from "@deck.gl/react";
import { PolygonLayer, ScatterplotLayer } from "@deck.gl/layers";
import { Map } from "@vis.gl/react-maplibre";
import "maplibre-gl/dist/maplibre-gl.css";
import { useEffect, useMemo, useState } from "react";
import { fetchLive, type LiveSnapshot } from "@/lib/live";
import { loadReplay, pendingAt, STATUS_COLORS, vehiclesAt, type Replay } from "@/lib/replay";
import { WaitChart } from "./WaitChart";

const MAP_STYLE = "https://basemaps.cartocdn.com/gl/dark-matter-nolabels-gl-style/style.json";
const VIEW = { longitude: -73.965, latitude: 40.745, zoom: 11.3, pitch: 0, bearing: 0 };
const SPEEDS = [10, 30, 60, 120];
const PICKUP_FADE_S = 25;

type Mode = "replay" | "live";

export default function Dashboard() {
  const [mode, setMode] = useState<Mode>("replay");
  return (
    <main className="shell">
      {mode === "replay" ? <ReplayView mode={mode} setMode={setMode} /> : <LiveView mode={mode} setMode={setMode} />}
    </main>
  );
}

function fmtClock(ms: number) {
  return new Date(ms).toISOString().slice(11, 19);
}

function fmtWait(s: number) {
  if (!isFinite(s) || s <= 0) return "–";
  return `${Math.floor(s / 60)}:${String(Math.round(s % 60)).padStart(2, "0")}`;
}

function ModeToggle({ mode, setMode }: { mode: Mode; setMode: (m: Mode) => void }) {
  return (
    <div className="seg" role="tablist" aria-label="Mode">
      {(["replay", "live"] as const).map((m) => (
        <button key={m} role="tab" aria-selected={mode === m} className={mode === m ? "on" : ""} onClick={() => setMode(m)}>
          {m === "replay" ? "Replay" : "Live"}
        </button>
      ))}
    </div>
  );
}

function Legend({ counts, waiting }: { counts: number[]; waiting: number }) {
  const rows: [string, [number, number, number], number][] = [
    ["Idle", STATUS_COLORS[1], counts[1]],
    ["To pickup", STATUS_COLORS[2], counts[2]],
    ["On trip", STATUS_COLORS[3], counts[3]],
    ["Rider waiting", [255, 93, 115], waiting],
  ];
  return (
    <ul className="legend">
      {rows.map(([label, c, n]) => (
        <li key={label}>
          <span className="dot" style={{ background: `rgb(${c.join(",")})` }} />
          <span className="lbl">{label}</span>
          <span className="num">{n.toLocaleString()}</span>
        </li>
      ))}
    </ul>
  );
}

function Header({ mode, setMode, sub }: { mode: Mode; setMode: (m: Mode) => void; sub: string }) {
  return (
    <header className="head">
      <div>
        <h1>Fleetline</h1>
        <p className="sub">{sub}</p>
      </div>
      <ModeToggle mode={mode} setMode={setMode} />
    </header>
  );
}

// ---------------------------------------------------------------- replay

function ReplayView({ mode, setMode }: { mode: Mode; setMode: (m: Mode) => void }) {
  const [replay, setReplay] = useState<Replay | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [t, setT] = useState(0);
  const [playing, setPlaying] = useState(true);
  const [speed, setSpeed] = useState(30);

  useEffect(() => {
    loadReplay().then(setReplay, (e) => setErr(String(e)));
  }, []);

  const duration = replay ? (replay.meta.frames - 1) * replay.meta.frame_s : 0;

  // Animation clock: sim seconds advance at `speed` per wall second, looping.
  useEffect(() => {
    if (!replay || !playing) return;
    let raf = 0;
    let last = performance.now();
    const tick = (now: number) => {
      const dt = (now - last) / 1000;
      last = now;
      setT((prev) => (prev + dt * speed) % duration);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [replay, playing, speed, duration]);

  const frame = useMemo(() => {
    if (!replay) return null;
    const n = replay.meta.vehicles;
    // Fresh arrays each frame so deck.gl sees new attribute data.
    const out = { pos: new Float32Array(2 * n), col: new Uint8Array(4 * n), counts: [0, 0, 0, 0] };
    vehiclesAt(replay, t, out);
    return { ...out, pending: pendingAt(replay, t) };
  }, [replay, t]);

  if (err) return <Notice title="Couldn't load the recorded run" body={err} />;
  if (!replay || !frame) return <Notice title="Loading a recorded day of NYC…" body="~10 MB of vehicle positions" />;

  const { meta } = replay;
  const recent = replay.pickups.filter((p) => p.t <= t && t - p.t < PICKUP_FADE_S);
  const minute = meta.minutes[Math.min(Math.floor(t / 60), meta.minutes.length - 1)];
  const totals = meta.minutes.reduce(
    (a, m) => ({ req: a.req + m.requests, uns: a.uns + m.unserved }),
    { req: 0, uns: 0 },
  );
  const avs = replay.isAV.reduce((a, b) => a + b, 0);

  const layers = [
    new PolygonLayer({
      id: "zone",
      data: [{ polygon: meta.robotaxi_zone }],
      getPolygon: (d: { polygon: [number, number][] }) => d.polygon,
      stroked: true,
      filled: true,
      getFillColor: [192, 132, 252, 14],
      getLineColor: [192, 132, 252, 140],
      lineWidthMinPixels: 1.2,
    }),
    new ScatterplotLayer({
      id: "vehicles",
      data: { length: meta.vehicles, attributes: { getPosition: { value: frame.pos, size: 2 }, getFillColor: { value: frame.col, size: 4 } } },
      radiusUnits: "pixels",
      getRadius: 1.6,
      radiusMinPixels: 1.2,
    }),
    new ScatterplotLayer({
      id: "pending",
      data: { length: frame.pending.length / 2, attributes: { getPosition: { value: frame.pending, size: 2 } } },
      radiusUnits: "pixels",
      getRadius: 2.6,
      getFillColor: [255, 93, 115, 235],
    }),
    new ScatterplotLayer({
      id: "pickups",
      data: recent,
      getPosition: (p: { lon: number; lat: number }) => [p.lon, p.lat],
      radiusUnits: "pixels",
      getRadius: (p: { t: number }) => 3 + ((t - p.t) / PICKUP_FADE_S) * 14,
      stroked: true,
      filled: false,
      getLineColor: (p: { t: number; av: boolean }) =>
        [...(p.av ? [192, 132, 252] : [255, 255, 255]), Math.round(200 * (1 - (t - p.t) / PICKUP_FADE_S))] as [number, number, number, number],
      lineWidthMinPixels: 1,
      updateTriggers: { getRadius: t, getLineColor: t },
    }),
  ];

  return (
    <>
      <DeckGL initialViewState={VIEW} controller layers={layers}>
        <Map mapStyle={MAP_STYLE} />
      </DeckGL>
      <aside className="panel">
        <Header mode={mode} setMode={setMode} sub={`Replaying real NYC Uber/Lyft requests · ${meta.day}`} />
        <div className="clock">
          <span className="time">{fmtClock(meta.start_ms + t * 1000)}</span>
          <span className="muted">simulated · {speed}×</span>
        </div>
        <Legend counts={frame.counts} waiting={frame.pending.length / 2} />
        <section className="card">
          <h2>Median wait, this minute</h2>
          <div className="kpis">
            <div>
              <span className="k sim">{fmtWait(minute.sim_p50_s)}</span>
              <span className="muted">Fleetline (greedy)</span>
            </div>
            <div>
              <span className="k real">{fmtWait(minute.real_p50_s)}</span>
              <span className="muted">Uber/Lyft, same requests</span>
            </div>
          </div>
          <WaitChart minutes={meta.minutes} t={t} />
          <p className="muted small">
            {totals.req.toLocaleString()} requests in the window · {((100 * totals.uns) / Math.max(1, totals.req)).toFixed(2)}% unserved ·{" "}
            {avs.toLocaleString()} robotaxis (purple zone)
          </p>
          <p className="muted small">
            Real waits include driver accept/decline time the simulator doesn&apos;t model yet, so Fleetline&apos;s line sits
            below the real one.
          </p>
        </section>
        <div className="controls">
          <button className="play" onClick={() => setPlaying((p) => !p)} aria-label={playing ? "Pause" : "Play"}>
            {playing ? "❚❚" : "▶"}
          </button>
          <input
            type="range"
            min={0}
            max={duration}
            step={1}
            value={Math.round(t)}
            onChange={(e) => setT(Number(e.target.value))}
            aria-label="Time"
          />
          <div className="seg small">
            {SPEEDS.map((s) => (
              <button key={s} className={speed === s ? "on" : ""} onClick={() => setSpeed(s)}>
                {s}×
              </button>
            ))}
          </div>
        </div>
        <footer className="foot muted small">
          Greedy dispatch · {meta.params.max_pickup} pickup cap · supply calibrated per hour ·{" "}
          {meta.vehicles.toLocaleString()} vehicles · deterministic offline run (seed {meta.params.seed})
        </footer>
      </aside>
    </>
  );
}

// ---------------------------------------------------------------- live

function LiveView({ mode, setMode }: { mode: Mode; setMode: (m: Mode) => void }) {
  const [snap, setSnap] = useState<LiveSnapshot | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const base = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("fleetd") ?? "http://localhost:9100" : "";

  useEffect(() => {
    let stop = false;
    const ctl = new AbortController();
    const poll = async () => {
      while (!stop) {
        try {
          setSnap(await fetchLive(base, ctl.signal));
          setErr(null);
        } catch (e) {
          if (!stop) setErr(String(e));
        }
        await new Promise((r) => setTimeout(r, 1000));
      }
    };
    poll();
    return () => {
      stop = true;
      ctl.abort();
    };
  }, [base]);

  const data = useMemo(() => {
    if (!snap) return null;
    const n = snap.vehicles.length / 4;
    const pos = new Float32Array(2 * n);
    const col = new Uint8Array(4 * n);
    const counts = [0, 0, 0, 0];
    for (let i = 0; i < n; i++) {
      const s = snap.vehicles[4 * i + 2];
      counts[s]++;
      pos[2 * i] = snap.vehicles[4 * i + 1];
      pos[2 * i + 1] = snap.vehicles[4 * i];
      const c = STATUS_COLORS[s] ?? [150, 150, 150];
      col.set([c[0], c[1], c[2], 220], 4 * i);
    }
    const pend = new Float32Array(snap.pending.length);
    for (let i = 0; i < snap.pending.length; i += 2) {
      pend[i] = snap.pending[i + 1];
      pend[i + 1] = snap.pending[i];
    }
    return { n, pos, col, counts, pend };
  }, [snap]);

  const layers = data
    ? [
        new ScatterplotLayer({
          id: "live-vehicles",
          data: { length: data.n, attributes: { getPosition: { value: data.pos, size: 2 }, getFillColor: { value: data.col, size: 4 } } },
          radiusUnits: "pixels",
          getRadius: 1.6,
        }),
        new ScatterplotLayer({
          id: "live-pending",
          data: { length: data.pend.length / 2, attributes: { getPosition: { value: data.pend, size: 2 } } },
          radiusUnits: "pixels",
          getRadius: 2.6,
          getFillColor: [255, 93, 115, 235],
        }),
      ]
    : [];

  return (
    <>
      <DeckGL initialViewState={VIEW} controller layers={layers}>
        <Map mapStyle={MAP_STYLE} />
      </DeckGL>
      <aside className="panel">
        <Header mode={mode} setMode={setMode} sub={`Live from fleetd · ${base}`} />
        {err || !snap || !data ? (
          <section className="card">
            <h2>Not connected</h2>
            <p className="muted small">
              Live mode reads the running system through fleetd&apos;s <code>/live</code> endpoint. Start it from the repo root with{" "}
              <code>make demo</code>, then come back. (Use <code>?fleetd=http://host:9100</code> for another address.)
            </p>
            {err && <p className="muted small">Last error: {err}</p>}
          </section>
        ) : (
          <>
            <div className="clock">
              <span className="time">{snap.sim_time.slice(11)}</span>
              <span className="muted">event time · Kafka → Redis</span>
            </div>
            <Legend counts={data.counts} waiting={data.pend.length / 2} />
            <section className="card">
              <h2>Since fleetd started</h2>
              <div className="kpis">
                <div>
                  <span className="k sim">{fmtWait(snap.wait_p50_s)}</span>
                  <span className="muted">median wait</span>
                </div>
                <div>
                  <span className="k">{fmtWait(snap.wait_p90_s)}</span>
                  <span className="muted">p90 wait</span>
                </div>
              </div>
              <p className="muted small">
                {snap.served.toLocaleString()} served · {(100 * snap.unserved_rate).toFixed(2)}% unserved
              </p>
            </section>
          </>
        )}
      </aside>
    </>
  );
}

function Notice({ title, body }: { title: string; body: string }) {
  return (
    <div className="notice">
      <h1>{title}</h1>
      <p className="muted">{body}</p>
    </div>
  );
}
