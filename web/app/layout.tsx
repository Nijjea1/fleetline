import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";

const sans = Inter({ subsets: ["latin"], variable: "--font-sans" });
const mono = JetBrains_Mono({ subsets: ["latin"], variable: "--font-mono" });

export const metadata: Metadata = {
  title: "Fleetline · real-time dispatch on a real NYC morning",
  description:
    "29,087 cars, 12,917 real Uber/Lyft requests: ride dispatch for a mixed fleet of human drivers and robotaxis. Go + Kafka + Redis streaming core.",
};

export const viewport: Viewport = { themeColor: "#0a0d12" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${sans.variable} ${mono.variable}`}>
      <body>{children}</body>
    </html>
  );
}
