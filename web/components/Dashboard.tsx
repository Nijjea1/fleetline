"use client";
import type { Layer, PickingInfo } from "@deck.gl/core";
import { PolygonLayer, ScatterplotLayer } from "@deck.gl/layers";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { clock12, longDay, mmss, n } from "@/lib/format";
import { fetchLive, type LiveSnapshot } from "@/lib/live";
import { loadReplay, pendingAt, vehiclesAt, type Replay } from "@/lib/replay";
import { RIDER, ROBOTAXI, STATUS_BY_CODE } from "@/lib/theme";
import { About } from "./About";
import { MapView } from "./MapView";
import { Brand, FleetStatus, MapKey, Section, type Mode } from "./Panel";
import { Timeline } from "./Timeline";
import { WaitChart } from "./WaitChart";

const PICKUP_FADE_S = 25;
const SEEN_KEY = "fleetline.seen-intro";

// Dots scale with zoom (meters), clamped so they stay visible zoomed out and don't turn
// into blobs zoomed in.
const CAR_STYLE = { radiusUnits: "meters", getRadius: 22, radiusMinPixels: 1.3, radiusMaxPixels: 6 } as const;
const RIDER_STYLE = {
  radiusUnits: "meters",
  getRadius: 34,
  radiusMinPixels: 2,
  radiusMaxPixels: 8,
  getFillColor: [...RIDER, 240] as [number, number, number, number],
} as const;

export default function Dashboard() {
  // ?mode=live opens live mode directly (shareable, and scriptable for screenshots).
  const [mode, setMode] = useState<Mode>(() =>
    typeof window !== "undefined" && new URLSearchParams(window.location.search).get("mode") === "live" ? "live" : "replay",
  );
  const [about, setAbout] = useState(false);
  useEffect(() => {
    if (!localStorage.getItem(SEEN_KEY) && !new URLSearchParams(window.location.search).has("nointro")) setAbout(true);
  }, []);
  const closeAbout = useCallback(() => {
    localStorage.setItem(SEEN_KEY, "1");
    setAbout(false);
  }, []);
  const openAbout = useCallback(() => setAbout(true), []);

  const brand = <Brand mode={mode} setMode={setMode} onAbout={openAbout} />;
  return (
    <main className="shell">
      {mode === "replay" ? <ReplayView brand={brand} onAbout={openAbout} /> : <LiveView brand={brand} />}
      <About open={about} onClose={closeAbout} />
    </main>
  );
}

function carLabel(status: number, av: boolean) {
  return `${av ? "Robotaxi" : "Human driver"} · ${STATUS_BY_CODE[status]?.label ?? "Offline"}`;
}

// ---------------------------------------------------------------- replay

function ReplayView({ brand, onAbout }: { brand: ReactNode; onAbout: () => void }) {
  const [replay, setReplay] = useState<Replay | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [t, setT] = useState(0);
  const [playing, setPlaying] = useState(true);
  const [speed, setSpeed] = useState(30);

  useEffect(() => {
    loadReplay().then(setReplay, (e) => setErr(String(e)));
  }, []);

  // Frames are snapshots at 0, 10, … s; the window they cover ends one frame later.
  const duration = replay ? (replay.meta.frames - 1) * replay.meta.frame_s : 0;
  const endMs = replay ? replay.meta.start_ms + replay.meta.frames * replay.meta.frame_s * 1000 : 0;

  // Animation clock: simulated seconds advance at `speed` per wall second, looping.
  useEffect(() => {
    if (!replay || !playing) return;
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
  }, [replay, playing, speed, duration]);

  const frame = useMemo(() => {
    if (!replay) return null;
    const count = replay.meta.vehicles;
    // Fresh arrays each frame so deck.gl sees new attribute data.
    const out = { pos: new Float32Array(2 * count), col: new Uint8Array(4 * count), counts: [0, 0, 0, 0] };
    vehiclesAt(replay, t, out);
    return { ...out, pending: pendingAt(replay, t) };
  }, [replay, t]);

  const tooltip = useCallback(
    (info: PickingInfo) => {
      if (!replay) return null;
      const id = info.layer?.id;
      if (id === "vehicles") {
        const f = replay.frames[Math.min(Math.floor(t / replay.meta.frame_s), replay.frames.length - 1)];
        return carLabel(f.status[info.index], replay.isAV[info.index] === 1);
      }
      if (id === "pending") return "Rider waiting for a car";
      if (id === "pickups") {
        const p = info.object as { wait: number; av: boolean };
        return `Picked up by a ${p.av ? "robotaxi" : "driver"} after a ${mmss(p.wait)} wait`;
      }
      return null;
    },
    [replay, t],
  );

  if (err) return <Notice title="Couldn't load the recording" body={err} />;
  if (!replay || !frame) return <Notice title="Loading a recorded NYC morning…" body="29,087 cars · 20 minutes · about 6 MB" spinner />;

  const { meta } = replay;
  const recent = replay.pickups.filter((p) => p.t <= t && t - p.t < PICKUP_FADE_S);
  const mi = Math.min(Math.floor(t / 60), meta.minutes.length - 1);
  const minute = meta.minutes[mi];

  const layers: Layer[] = [
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
      getPosition: (p: { lon: number; lat: number }) => [p.lon, p.lat],
      radiusUnits: "pixels",
      getRadius: (p: { t: number }) => 3 + ((t - p.t) / PICKUP_FADE_S) * 12,
      stroked: true,
      filled: false,
      getLineColor: (p: { t: number; av: boolean }) =>
        [...(p.av ? ROBOTAXI : [255, 255, 255]), Math.round(170 * (1 - (t - p.t) / PICKUP_FADE_S))] as [number, number, number, number],
      lineWidthMinPixels: 1,
      pickable: true,
      updateTriggers: { getRadius: t, getLineColor: t },
    }),
  ];

  return (
    <>
      <MapView layers={layers} tooltip={tooltip} />
      <aside className="panel">
        {brand}
        <p className="context">
          {longDay(meta.day)} · {clock12(meta.start_ms, false)}–{clock12(endMs, false)} ·{" "}
          {n(meta.summary.requests)} real ride requests
        </p>

        <Section title="Median rider wait" aside={`at ${clock12(meta.start_ms + mi * 60_000, false)}`}>
          <div className="kpis">
            <div className="kpi">
              <span className="kpi-val">{mmss(minute.sim_p50_s)}</span>
              <span className="kpi-lbl">
                <i className="lg sim" /> Fleetline (simulated)
              </span>
            </div>
            <div className="kpi">
              <span className="kpi-val dim">{mmss(minute.real_p50_s)}</span>
              <span className="kpi-lbl">
                <i className="lg real" /> Actual Uber/Lyft
              </span>
            </div>
          </div>
          <WaitChart minutes={meta.minutes} t={t} startMs={meta.start_ms} />
          <p className="note">
            Not a like-for-like win: real waits include drivers accepting a request, which isn&apos;t simulated yet.{" "}
            <button className="link" onClick={onAbout}>
              More
            </button>
          </p>
        </Section>

        <FleetStatus counts={frame.counts} waiting={frame.pending.length / 2} />
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

// ---------------------------------------------------------------- live

function LiveView({ brand }: { brand: ReactNode }) {
  const [snap, setSnap] = useState<LiveSnapshot | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const base = typeof window !== "undefined" ? (new URLSearchParams(window.location.search).get("fleetd") ?? "http://localhost:9100") : "";

  useEffect(() => {
    let stop = false;
    const ctl = new AbortController();
    (async () => {
      while (!stop) {
        try {
          setSnap(await fetchLive(base, ctl.signal));
          setErr(null);
        } catch (e) {
          if (!stop) setErr(String(e));
        }
        await new Promise((r) => setTimeout(r, 1000));
      }
    })();
    return () => {
      stop = true;
      ctl.abort();
    };
  }, [base]);

  const data = useMemo(() => {
    if (!snap) return null;
    const count = snap.vehicles.length / 4;
    const pos = new Float32Array(2 * count);
    const col = new Uint8Array(4 * count);
    const status = new Uint8Array(count);
    const av = new Uint8Array(count);
    const counts = [0, 0, 0, 0];
    for (let i = 0; i < count; i++) {
      const s = snap.vehicles[4 * i + 2];
      status[i] = s;
      av[i] = snap.vehicles[4 * i + 3];
      counts[s]++;
      pos[2 * i] = snap.vehicles[4 * i + 1];
      pos[2 * i + 1] = snap.vehicles[4 * i];
      const c = STATUS_BY_CODE[s]?.rgb;
      if (c) col.set([c[0], c[1], c[2], 220], 4 * i);
    }
    const pend = new Float32Array(snap.pending.length);
    for (let i = 0; i < snap.pending.length; i += 2) {
      pend[i] = snap.pending[i + 1];
      pend[i + 1] = snap.pending[i];
    }
    return { count, pos, col, status, av, counts, pend };
  }, [snap]);

  const tooltip = useCallback(
    (info: PickingInfo) => {
      if (!data) return null;
      if (info.layer?.id === "live-vehicles") return carLabel(data.status[info.index], data.av[info.index] === 1);
      if (info.layer?.id === "live-pending") return "Rider waiting for a car";
      return null;
    },
    [data],
  );

  const layers: Layer[] = data
    ? [
        new ScatterplotLayer({
          id: "live-vehicles",
          data: { length: data.count, attributes: { getPosition: { value: data.pos, size: 2 }, getFillColor: { value: data.col, size: 4 } } },
          ...CAR_STYLE,
          pickable: true,
        }),
        new ScatterplotLayer({
          id: "live-pending",
          data: { length: data.pend.length / 2, attributes: { getPosition: { value: data.pend, size: 2 } } },
          ...RIDER_STYLE,
          pickable: true,
        }),
      ]
    : [];

  return (
    <>
      <MapView layers={layers} tooltip={tooltip} />
      <aside className="panel">
        {brand}
        {!err && snap && data ? (
          <>
            <p className="context">
              <span className="pill ok">● Live</span> Streaming from fleetd at {base.replace(/^https?:\/\//, "")}
            </p>
            <Section title="Rider wait so far" aside={`${n(snap.served)} served`}>
              <div className="kpis">
                <div className="kpi">
                  <span className="kpi-val">{mmss(snap.wait_p50_s)}</span>
                  <span className="kpi-lbl">Median</span>
                </div>
                <div className="kpi">
                  <span className="kpi-val dim">{mmss(snap.wait_p90_s)}</span>
                  <span className="kpi-lbl">90th percentile</span>
                </div>
              </div>
              <p className="note">
                {snap.served
                  ? `${(100 * snap.unserved_rate).toFixed(2)}% of requests went unserved.`
                  : "The simulator is warming up the fleet; wait times appear once the measured window starts."}
              </p>
            </Section>
            <FleetStatus counts={data.counts} waiting={data.pend.length / 2} />
            <MapKey replay={false} />
          </>
        ) : (
          <>
            <p className="context">
              <span className="pill">○ Not connected</span>
            </p>
            <Section title="Run the live system">
              <p className="body">
                Live mode shows the full streaming stack (Kafka → Go dispatcher → Redis) running on <i>your</i> machine. This page
                can&apos;t reach it from the internet, so start it locally:
              </p>
              <ol className="steps">
                <li>Clone the repo and prepare the data (see the README)</li>
                <li>
                  Run <code>make demo</code> from the repo root
                </li>
                <li>This page connects on its own within a second</li>
              </ol>
              <p className="note">
                Looking for <code>{base}/live</code>. Add <code>?fleetd=http://host:9100</code> for another address.
              </p>
            </Section>
          </>
        )}
      </aside>
      {!err && snap && data && (
        <div className="timeline live-bar">
          <span className="pulse" aria-hidden />
          <div className="tl-time">
            <span className="tl-clock">{clock12(Date.parse(snap.sim_time.replace(" ", "T") + "Z"))}</span>
            <span className="tl-day">event time · {longDay(snap.sim_time.slice(0, 10))}</span>
          </div>
          <span className="live-note">{n(data.count)} cars read from Redis · refreshed every second</span>
        </div>
      )}
    </>
  );
}

function Notice({ title, body, spinner }: { title: string; body: string; spinner?: boolean }) {
  return (
    <div className="notice">
      {spinner && <div className="spinner" aria-hidden />}
      <h1>{title}</h1>
      <p className="muted">{body}</p>
    </div>
  );
}
