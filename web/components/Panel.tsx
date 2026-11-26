"use client";
import type { ReactNode } from "react";
import { n, pct } from "@/lib/format";
import { css, RIDER, ROBOTAXI, STATUS } from "@/lib/theme";

export type Mode = "replay" | "compare" | "live";

export function Brand({ mode, setMode, onAbout }: { mode: Mode; setMode: (m: Mode) => void; onAbout: () => void }) {
  return (
    <header className="brand">
      <div className="brand-row">
        <div className="logo" aria-hidden>
          <span />
        </div>
        <div className="brand-name">Fleetline</div>
        <button className="icon-btn" onClick={onAbout} aria-label="About this project" title="What am I looking at?">
          ?
        </button>
      </div>
      <p className="tagline">Real-time ride dispatch, replaying real New York City Uber &amp; Lyft demand.</p>
      <div className="seg seg-full" role="tablist" aria-label="View">
        {(
          [
            ["replay", "Replay"],
            ["compare", "Compare"],
            ["live", "Live"],
          ] as const
        ).map(([m, label]) => (
          <button key={m} role="tab" aria-selected={mode === m} className={mode === m ? "on" : ""} onClick={() => setMode(m)}>
            {label}
          </button>
        ))}
      </div>
    </header>
  );
}

export function Section({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <section className="section">
      <div className="section-head">
        <h2>{title}</h2>
        {aside && <span className="section-aside">{aside}</span>}
      </div>
      {children}
    </section>
  );
}

/** Where the fleet is right now: one stacked bar plus a row per status (doubles as the map key). */
export function FleetStatus({ counts, waiting }: { counts: number[]; waiting: number }) {
  const rows = [STATUS.idle, STATUS.pickup, STATUS.trip].map((s) => ({ ...s, value: counts[s.code] ?? 0 }));
  const online = rows.reduce((a, r) => a + r.value, 0);
  return (
    <Section title="Fleet right now" aside={`${n(online)} cars online`}>
      <div className="stack" role="img" aria-label="Share of online cars by status">
        {rows.map((r) => (
          <span key={r.code} style={{ flexGrow: r.value, background: css(r.rgb) }} />
        ))}
      </div>
      <ul className="rows">
        {rows.map((r) => (
          <li key={r.code}>
            <span className="swatch" style={{ background: css(r.rgb) }} />
            <span className="row-label">{r.label}</span>
            <span className="row-num">{n(r.value)}</span>
            <span className="row-pct">{pct(r.value, online)}</span>
          </li>
        ))}
        <li className="rider">
          <span className="swatch ring" style={{ background: css(RIDER), boxShadow: `0 0 0 3px ${css(RIDER, 0.25)}` }} />
          <span className="row-label">Riders waiting for a car</span>
          <span className="row-num">{n(waiting)}</span>
          <span className="row-pct" />
        </li>
      </ul>
    </Section>
  );
}

export function MapKey({ replay }: { replay: boolean }) {
  return (
    <Section title="On the map">
      <ul className="key">
        <li>
          <span className="key-dot" style={{ background: css([200, 205, 215]) }} />
          Each dot is one car, colored by status above
        </li>
        {replay && (
          <li>
            <span className="key-ring" />A ring = a rider was just picked up
          </li>
        )}
        <li>
          <span className="key-area" style={{ borderColor: css(ROBOTAXI), background: css(ROBOTAXI, 0.12) }} />
          Robotaxi service area (Manhattan)
        </li>
      </ul>
      <p className="hint">Hover over any dot for details · scroll to zoom · drag to pan</p>
    </Section>
  );
}
