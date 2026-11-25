import type { NextConfig } from "next";

// Static export: the replay demo is plain files (HTML + JS + the recorded run), so it can
// be hosted anywhere (Vercel, GitHub Pages, S3) with no server.
const config: NextConfig = { output: "export", images: { unoptimized: true } };

export default config;
