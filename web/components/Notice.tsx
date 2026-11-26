"use client";

/** Full-screen loading / error message. */
export function Notice({ title, body, spinner }: { title: string; body: string; spinner?: boolean }) {
  return (
    <div className="notice">
      {spinner && <div className="spinner" aria-hidden />}
      <h1>{title}</h1>
      <p className="muted">{body}</p>
    </div>
  );
}
