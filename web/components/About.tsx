"use client";
import { useEffect, useRef } from "react";

/** "What am I looking at?": opens on the first visit, and from the ? button after that. */
export function About({ open, onClose }: { open: boolean; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);

  return (
    <dialog ref={ref} className="about" onClose={onClose} onClick={(e) => e.target === ref.current && onClose()}>
      <div className="about-body">
        <p className="eyebrow">What you&apos;re looking at</p>
        <h1>One real New York morning, re-dispatched by Fleetline</h1>
        <p className="lead">
          On Wednesday, Sep 16, 2026, riders in New York requested <b>12,917</b> Uber and Lyft trips between 8:00 and 8:20 AM.
          This map replays every one of them, at the real time and place, and lets Fleetline decide which car picks each
          rider up.
        </p>

        <div className="about-grid">
          <div>
            <h3>The dots</h3>
            <p>
              <b>29,087 cars</b>: human drivers everywhere, plus robotaxis that only serve the purple Manhattan zone. Blue cars
              are free, amber ones are heading to a rider, green ones are carrying one. Red dots are riders still waiting.
            </p>
          </div>
          <div>
            <h3>The comparison</h3>
            <p>
              The panel compares Fleetline&apos;s median wait with what riders <i>actually</i> waited. It isn&apos;t apples to
              apples: real waits include drivers accepting or declining requests, which the simulator doesn&apos;t model yet.
              So treat the gap as an upper bound, not a win.
            </p>
          </div>
          <div>
            <h3>How it&apos;s built</h3>
            <p>
              A Go service reads vehicle GPS pings and ride requests from Kafka (Redpanda), keeps fleet state in Redis, and
              assigns the nearest suitable car every second. This recording comes from the same dispatch code run as a
              deterministic simulation; <b>Live system</b> connects to the full streaming stack on your machine.
            </p>
          </div>
          <div>
            <h3>Measured</h3>
            <ul>
              <li>187k GPS pings/s ingested on one laptop</li>
              <li>0 pings lost with Redis frozen or killed</li>
              <li>Simulated median wait within 3% of real over a full day</li>
            </ul>
          </div>
        </div>

        <div className="about-foot">
          <span className="muted">Data: NYC Taxi &amp; Limousine Commission trip records · Map © OpenStreetMap, CARTO</span>
          <button className="btn-primary" onClick={onClose} autoFocus>
            Explore the map
          </button>
        </div>
      </div>
    </dialog>
  );
}
