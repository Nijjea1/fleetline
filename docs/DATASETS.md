# Datasets

Every dataset Fleetline uses, what it is for, and what we verified about it. "Verified" means we read the file's schema ourselves, not just a blog post.

## Summary

| Role | Dataset | Status |
| --- | --- | --- |
| **Primary demand** | NYC TLC High-Volume FHV (Uber/Lyft) trips | Core, Phase 1 |
| Zone geometry | NYC TLC Taxi Zones shapefile | Core, Phase 1 |
| Road network | OpenStreetMap NYC extract (Geofabrik) → OSRM | Phase 3 |
| ETA features | Hourly weather (NOAA / Open-Meteo, Central Park) | Phase 3 |
| GPS-level ETA check | NYC Taxi Trip Duration (Kaggle, 2016, lat/lon) | Phase 3, optional |
| Delivery routing | Amazon Last Mile Routing Challenge (9,184 routes) | Phase 3 (E7) |
| Delivery dispatch (alt) | Meituan × INFORMS TSL 2024 challenge data | Optional |
| Second city | Chicago TNP trips (2025-) | Optional |
| Robotaxi | Waymo Open Motion Dataset | Phase 4 stretch (E8) |

## Primary demand: NYC High-Volume FHV (HVFHV)

The scope doc said "NYC TLC trips". We are specifically using the **High-Volume For-Hire Vehicle** files (Uber, Lyft, Via) instead of yellow taxi data.

**Why this is the best fit for a dispatch project:** these are real app-dispatched rides, and they carry a `request_datetime` *and* an `on_scene_datetime`. The gap between them is the **real rider wait time Uber/Lyft achieved**. That gives Fleetline a real-world reference line: "the production dispatcher got riders a car in X minutes; my simulated greedy got Y; batching got Z." No yellow-taxi dataset has request times, because taxis are street-hailed.

Verified schema (`fhvhv_tripdata_2026-08.parquet`, 20,493,480 rows, ~500 MB):

```
hvfhs_license_num, dispatching_base_num, originating_base_num,
request_datetime, on_scene_datetime, pickup_datetime, dropoff_datetime,
PULocationID, DOLocationID, trip_miles, trip_time,
base_passenger_fare, tolls, bcf, sales_tax, congestion_surcharge, airport_fee,
tips, driver_pay, shared_request_flag, shared_match_flag,
access_a_ride_flag, wav_request_flag, wav_match_flag, cbd_congestion_fee
```

- `hvfhs_license_num`: HV0003 = Uber, HV0005 = Lyft (per TLC data dictionary).
- Source: `https://d37ci6vzurychx.cloudfront.net/trip-data/fhvhv_tripdata_YYYY-MM.parquet`
- Published monthly with roughly a two-month lag.

**Known limitation: no coordinates.** Locations are one of 263 taxi zone IDs. We checked: the TLC's republished 2015 yellow-taxi parquet files *also* use zone IDs now (`yellow_tripdata_2015-03.parquet` has `PULocationID`, no lat/lon), so "use older data with GPS" is not an option from the official source. We handle this by sampling a point inside each zone polygon with a fixed seed (see `DECISIONS.md`, D-003).

**No driver data.** Trips tell us demand, not supply. Fleet size, driver start positions and repositioning behavior are simulator parameters.

## Zone geometry: NYC Taxi Zones

`https://d37ci6vzurychx.cloudfront.net/misc/taxi_zones.zip` (~1 MB shapefile). Polygons for the 263 zones, used to sample pickup/dropoff points and to draw the dashboard.

## Road network: OpenStreetMap + OSRM (Phase 3)

Geofabrik's New York extract, preprocessed by OSRM in Docker, gives real road travel times. This becomes the routing-only ETA that the ML model corrects.

## Weather (Phase 3)

Rain and snow change both demand and travel time. Hourly Central Park observations (NOAA, or Open-Meteo's free historical API) are cheap extra features for the ETA model and a nice ablation ("does weather help?").

## GPS-level check: Kaggle NYC Taxi Trip Duration (optional)

~1.46M yellow-taxi trips from early 2016 with exact pickup/dropoff lat/lon and trip duration. Requires a Kaggle login. Useful to sanity-check that the ETA model's zone-sampling approach is not hiding error that real coordinates would expose.

## Delivery: Amazon Last Mile (Phase 3, E7)

9,184 real Amazon delivery routes with stops, packages, time windows and the driver's actual sequence. License CC BY-NC 4.0. `https://registry.opendata.aws/amazon-last-mile-challenges`

## Delivery dispatch alternative: Meituan × INFORMS TSL (optional)

Eight days of real food-delivery dispatch from one city (Oct 2022), including order creation and dispatch timestamps and ~654k delivery offers, with privacy-shifted coordinates. Academic use only. It is the closest public match to DoorDash's dispatch problem. Only add this if Phases 1–3 finish on time.

## Second city: Chicago TNP (optional)

Chicago publishes all rideshare trips (2025- dataset `6dvr-xwnh`), located by census tract (finer than NYC's 263 zones) with times rounded to 15 minutes. Good for a "does the dispatcher generalize to another city?" experiment, but the 15-minute rounding makes it worse than NYC for wait-time replay. Not in the critical path.

## Robotaxi: Waymo Open Motion Dataset (Phase 4 stretch)

Real agent trajectories for motion prediction. Non-commercial; accept terms to download.

## Licensing rule

Raw and derived data are **never committed** (`data/` is gitignored). Scripts in `sim/` download and rebuild everything from the URLs above, so the repo stays small and we never redistribute licensed data.
