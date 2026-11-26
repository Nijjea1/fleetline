"use client";
import {
  WebMercatorViewport,
  type Layer,
  type MapViewState,
  type PickingInfo,
} from "@deck.gl/core";
import DeckGL from "@deck.gl/react";
import { Map } from "@vis.gl/react-maplibre";
import * as maplibregl from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { useEffect, useState } from "react";

// MapLibre v6 finds its worker next to its own script, which the bundler moved, so tiles
// were never parsed and the basemap stayed black. scripts/copy-maplibre-worker.mjs ships
// the worker as a static file; point MapLibre at it before any map is created.
if (typeof window !== "undefined") {
  maplibregl.setWorkerUrl(
    new URL("/maplibre-gl-worker.mjs", window.location.href).href,
  );
}

// Layer order, bottom to top: dark basemap without labels (MapLibre) → our data (deck.gl)
// → a second, transparent MapLibre map that draws only the labels, following the same
// camera. Labels above 29k dots keep street and neighborhood names readable. (deck.gl's
// "interleaved" mode would do this in one map, but deck 9.4 reads MapLibre v5 internals
// and crashes on v6; CARTO's raster label tiles now need an API key.)
const BASE_STYLE =
  "https://basemaps.cartocdn.com/gl/dark-matter-nolabels-gl-style/style.json";
const FULL_STYLE =
  "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json";
const NYC_BOUNDS: [[number, number], [number, number]] = [
  [-74.05, 40.57],
  [-73.76, 40.89],
];

type Style = { layers: { type: string }[] } & Record<string, unknown>;

/** The full CARTO style reduced to its label (symbol) layers on a transparent background. */
function useLabelsStyle() {
  const [style, setStyle] = useState<Style>();
  useEffect(() => {
    fetch(FULL_STYLE)
      .then((r) => r.json() as Promise<Style>)
      .then((full) =>
        setStyle({
          ...full,
          layers: full.layers.filter((l) => l.type === "symbol"),
        }),
      )
      .catch(() => {}); // labels are a nicety; the map still works without them
  }, []);
  return style;
}

export type Tooltip = (info: PickingInfo) => string | null;

type Padding = { left: number; right: number; top: number; bottom: number };

/** Camera that frames the city inside a width x height area, leaving `padding` free. */
export function fitNYC(
  width: number,
  height: number,
  padding: Padding,
): MapViewState {
  const { longitude, latitude, zoom } = new WebMercatorViewport({
    width,
    height,
  }).fitBounds(NYC_BOUNDS, { padding });
  return { longitude, latitude, zoom, pitch: 0, bearing: 0 };
}

function initialView(): MapViewState {
  const w = window.innerWidth;
  const h = window.innerHeight;
  // Leave room for the side panel and the bottom timeline when framing the city.
  const padding =
    w > 900
      ? { left: 400, right: 60, top: 40, bottom: 110 }
      : { left: 20, right: 20, top: 20, bottom: h * 0.5 };
  return fitNYC(w, h, padding);
}

/**
 * One map. Uncontrolled by default; pass `view` + `onView` to drive the camera from outside
 * (the compare view keeps two maps on one shared camera). `chrome` = zoom buttons + credits.
 */
export function MapView({
  layers,
  tooltip,
  view: outer,
  onView,
  chrome = true,
}: {
  layers: Layer[];
  tooltip?: Tooltip;
  view?: MapViewState;
  onView?: (v: MapViewState) => void;
  chrome?: boolean;
}) {
  const [own, setOwn] = useState<MapViewState>(() => outer ?? initialView());
  const view = outer ?? own;
  const setView = (f: MapViewState | ((v: MapViewState) => MapViewState)) => {
    const next = typeof f === "function" ? f(view) : f;
    if (onView) onView(next);
    else setOwn(next);
  };
  const labelsStyle = useLabelsStyle();
  const zoomBy = (d: number) =>
    setView((v) => ({ ...v, zoom: v.zoom + d, transitionDuration: 250 }));

  return (
    <>
      <DeckGL
        viewState={view}
        onViewStateChange={(e) => setView(e.viewState as MapViewState)}
        controller={{ dragRotate: false, touchRotate: false }}
        layers={layers}
        getTooltip={(info) => {
          const text = info.picked && tooltip ? tooltip(info) : null;
          return text ? { text, className: "map-tip" } : null;
        }}
        getCursor={({ isDragging, isHovering }) =>
          isDragging ? "grabbing" : isHovering ? "pointer" : "grab"
        }
      >
        <Map
          mapLib={maplibregl}
          mapStyle={BASE_STYLE}
          attributionControl={false}
        />
      </DeckGL>
      {labelsStyle && (
        <div className="labels" aria-hidden>
          <Map
            mapLib={maplibregl}
            mapStyle={labelsStyle as never}
            longitude={view.longitude}
            latitude={view.latitude}
            zoom={view.zoom}
            interactive={false}
            attributionControl={false}
          />
        </div>
      )}
      {chrome && (
        <>
          <div className="zoom" role="group" aria-label="Zoom">
            <button onClick={() => zoomBy(1)} aria-label="Zoom in">
              +
            </button>
            <button onClick={() => zoomBy(-1)} aria-label="Zoom out">
              −
            </button>
          </div>
          <div className="attrib">
            ©{" "}
            <a
              href="https://www.openstreetmap.org/copyright"
              target="_blank"
              rel="noreferrer"
            >
              OpenStreetMap
            </a>{" "}
            ©{" "}
            <a
              href="https://carto.com/attributions"
              target="_blank"
              rel="noreferrer"
            >
              CARTO
            </a>{" "}
            · Trips:{" "}
            <a
              href="https://www.nyc.gov/site/tlc/about/tlc-trip-record-data.page"
              target="_blank"
              rel="noreferrer"
            >
              NYC TLC
            </a>
          </div>
        </>
      )}
    </>
  );
}
