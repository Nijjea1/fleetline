"""Taxi-zone geometry and seeded point sampling (decision D-003).

TLC trips only say which of 263 zones a ride started/ended in. To get realistic
distances we place each trip at a uniformly random point *inside* its zone polygon.

Sampling is done in the shapefile's native CRS (EPSG:2263, NY State Plane, units = feet),
where equal areas are equal, then converted to WGS84 lon/lat for everything downstream.
"""

from pathlib import Path

import geopandas as gpd
import numpy as np
import shapely
from pyproj import Transformer

NATIVE_CRS = "EPSG:2263"
WGS84 = "EPSG:4326"
_to_wgs84 = Transformer.from_crs(NATIVE_CRS, WGS84, always_xy=True)


def load_zones(zip_path: Path) -> gpd.GeoDataFrame:
    """Load the zone shapefile, one row per LocationID, in native (feet) CRS."""
    gdf = gpd.read_file(f"zip://{zip_path}").to_crs(NATIVE_CRS)
    # A few LocationIDs appear as multiple rows (islands); merge them into one geometry.
    gdf = gdf.dissolve(by="LocationID", as_index=False)[["LocationID", "zone", "borough", "geometry"]]
    gdf["LocationID"] = gdf["LocationID"].astype(int)
    return gdf.sort_values("LocationID").reset_index(drop=True)


def sample_points_in_polygon(
    polygon: shapely.Geometry, n: int, rng: np.random.Generator
) -> tuple[np.ndarray, np.ndarray]:
    """Rejection sampling: draw in the bounding box, keep points that land inside.

    Vectorized with shapely.contains_xy so a zone with 10k trips is one or two numpy
    batches, not 10k Python calls.
    """
    if n == 0:
        return np.empty(0), np.empty(0)
    minx, miny, maxx, maxy = polygon.bounds
    shapely.prepare(polygon)
    xs, ys = [], []
    have = 0
    while have < n:
        # Oversample so most zones finish in one pass; zones fill ~30-70% of their bbox.
        k = max(64, int((n - have) * 2.5))
        x = rng.uniform(minx, maxx, k)
        y = rng.uniform(miny, maxy, k)
        inside = shapely.contains_xy(polygon, x, y)
        xs.append(x[inside])
        ys.append(y[inside])
        have += int(inside.sum())
    return np.concatenate(xs)[:n], np.concatenate(ys)[:n]


def to_lonlat(x: np.ndarray, y: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    lon, lat = _to_wgs84.transform(x, y)
    return np.asarray(lon), np.asarray(lat)
