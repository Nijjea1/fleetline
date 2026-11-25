"use client";
import { useEffect } from "react";
import { clock12 } from "@/lib/format";

const SPEEDS = [10, 30, 60, 120];

/** Media-style playback bar: play/pause, current time, scrubber, speed. Space / ← → work too. */
export function Timeline(p: {
  t: number;
  duration: number;
  startMs: number;
  endMs: number;
  day: string;
  playing: boolean;
  speed: number;
  onPlaying: (v: boolean) => void;
  onSeek: (t: number) => void;
  onSpeed: (s: number) => void;
}) {
  const { t, duration, startMs, playing, onPlaying, onSeek } = p;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (el.closest("input, textarea, dialog[open]")) return;
      if (e.code === "Space") {
        e.preventDefault();
        onPlaying(!playing);
      } else if (e.key === "ArrowRight") onSeek(Math.min(duration, t + 30));
      else if (e.key === "ArrowLeft") onSeek(Math.max(0, t - 30));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [t, duration, playing, onPlaying, onSeek]);

  const progress = duration ? (100 * t) / duration : 0;

  return (
    <div className="timeline" role="group" aria-label="Playback">
      <button className="play" onClick={() => onPlaying(!playing)} aria-label={playing ? "Pause" : "Play"} title="Space">
        {playing ? (
          <svg viewBox="0 0 16 16" aria-hidden>
            <rect x="3.5" y="2.5" width="3" height="11" rx="1" />
            <rect x="9.5" y="2.5" width="3" height="11" rx="1" />
          </svg>
        ) : (
          <svg viewBox="0 0 16 16" aria-hidden>
            <path d="M4.5 2.8v10.4a.8.8 0 0 0 1.2.7l8.4-5.2a.8.8 0 0 0 0-1.4L5.7 2.1a.8.8 0 0 0-1.2.7z" />
          </svg>
        )}
      </button>
      <div className="tl-time">
        <span className="tl-clock">{clock12(startMs + t * 1000)}</span>
        <span className="tl-day">{p.day}</span>
      </div>
      <div className="tl-track">
        <input
          type="range"
          min={0}
          max={duration}
          step={1}
          value={Math.round(t)}
          onChange={(e) => onSeek(Number(e.target.value))}
          aria-label="Time in the recording"
          style={{ ["--p" as string]: `${progress}%` }}
        />
        <div className="tl-ends">
          <span>{clock12(startMs, false)}</span>
          <span>{clock12(p.endMs, false)}</span>
        </div>
      </div>
      <div className="seg" role="radiogroup" aria-label="Playback speed">
        {SPEEDS.map((s) => (
          <button key={s} role="radio" aria-checked={p.speed === s} className={p.speed === s ? "on" : ""} onClick={() => p.onSpeed(s)}>
            {s}×
          </button>
        ))}
      </div>
    </div>
  );
}
