"use client";
import dynamic from "next/dynamic";

// deck.gl and MapLibre need the browser (WebGL), so skip server rendering.
const Dashboard = dynamic(() => import("@/components/Dashboard"), { ssr: false });

export default function Page() {
  return <Dashboard />;
}
