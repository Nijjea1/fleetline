// Display helpers. Recorded times are NYC wall-clock times stored as UTC milliseconds,
// so always read them with the UTC getters (the viewer's time zone must not shift them).

export function clock12(ms: number, seconds = true): string {
  const d = new Date(ms);
  const h = d.getUTCHours();
  const m = String(d.getUTCMinutes()).padStart(2, "0");
  const s = String(d.getUTCSeconds()).padStart(2, "0");
  return `${h % 12 || 12}:${m}${seconds ? `:${s}` : ""} ${h < 12 ? "AM" : "PM"}`;
}

export function longDay(isoDay: string): string {
  return new Date(`${isoDay}T12:00:00Z`).toLocaleDateString("en-US", {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });
}

/** 196 -> "3:16" (minutes:seconds); "–" when there's no value yet. */
export function mmss(s: number): string {
  if (!isFinite(s) || s <= 0) return "–";
  const r = Math.round(s);
  return `${Math.floor(r / 60)}:${String(r % 60).padStart(2, "0")}`;
}

export const n = (x: number) => x.toLocaleString("en-US");
export const pct = (x: number, total: number) => (total > 0 ? `${Math.round((100 * x) / total)}%` : "–");
