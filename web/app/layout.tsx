import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Fleetline · real-time dispatch on a real NYC day",
  description:
    "Ride-hail dispatch for a mixed fleet of human drivers and robotaxis, replaying real NYC Uber/Lyft demand. Go + Kafka + Redis streaming core.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
