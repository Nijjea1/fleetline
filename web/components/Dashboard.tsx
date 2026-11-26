"use client";
import type { Layer, PickingInfo } from "@deck.gl/core";
import { ScatterplotLayer } from "@deck.gl/layers";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { clock12, longDay, mmss, n } from "@/lib/format";
import { fetchLive, type LiveSnapshot } from "@/lib/live";
import { loadReplay, type Replay } from "@/lib/replay";
import { STATUS_BY_CODE } from "@/lib/theme";
import { About } from "./About";
import { CompareView } from "./Compare";
import { Notice } from "./Notice";
import { MapView } from "./MapView";
import { Brand, FleetStatus, MapKey, Section, type Mode } from "./Panel";
import { Timeline } from "./Timeline";
import { WaitChart } from "./WaitChart";
import { CAR_STYLE, carLabel, replayLayers, replayTooltip, RIDER_STYLE, useClock, useFrame } from "./replayLayers";

const SEEN_KEY = "fleetline.seen-intro";

export default function Dashboard() {
  // ?mode=live / ?mode=compare open that view directly (shareable, scriptable for screenshots).
  const [mode, setMode] = useState<Mode>(() => {
    const m = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("mode") : null;
    return m === "live" || m === "compare" ? m : "replay";
  });
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
      {mode === "replay" && <ReplayView brand={brand} onAbout={openAbout} />}
      {mode === "compare" && <CompareView brand={brand} />}
      {mode === "live" && <LiveView brand={brand} />}
      <About open={about} onClose={closeAbout} />
    </main>
  );
}

// ---------------------------------------------------------------- replay

function ReplayView({ brand, onAbout }: { brand: ReactNode; onAbout: () => void }) {
  const [replay, setReplay] = useState<Replay | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    loadReplay().then(setReplay, (e) => setErr(String(e)));
  }, []);

  // Frames are snapshots at 0, 10, … s; the window they cover ends one frame later.
  const duration = replay ? (replay.meta.frames - 1) * replay.meta.frame_s : 0;
  const endMs = replay ? replay.meta.start_ms + replay.meta.frames * replay.meta.frame_s * 1000 : 0;
  const { t, setT, playing, setPlaying, speed, setSpeed } = useClock(duration);
  const frame = useFrame(replay, t);
  const tooltip = useMemo(() => (replay ? replayTooltip(replay, t) : undefined), [replay, t]);

  if (err) return <Notice title="Couldn't load the recording" body={err} />;
  if (!replay || !frame) return <Notice title="Loading a recorded NYC morning…" body="29,087 cars · 20 minutes · about 6 MB" spinner />;

  const { meta } = replay;
  const mi = Math.min(Math.floor(t / 60), meta.minutes.length - 1);
  const minute = meta.minutes[mi];
  const layers = replayLayers(replay, frame, t);

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
          <WaitChart
            series={[
              { values: meta.minutes.map((m) => m.real_p50_s), style: "ref" },
              { values: meta.minutes.map((m) => m.sim_p50_s), style: "main" },
            ]}
            t={t}
            startMs={meta.start_ms}
            label="Median rider wait per minute, Fleetline vs actual"
          />
          <p className="note">
            Not a like-for-like win: real waits include drivers accepting a request, which this run doesn&apos;t model.{" "}
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
