"""E5 baselines, simplest first. Every ML gain is measured against these.

B0 OSRM free-flow time, untouched.
B1 OSRM x one traffic factor (median of actual/OSRM on train).
B2 OSRM x a factor per hour of the week (168 factors).
B3 LightGBM on all request-time features, L1 objective (optimizes MAE directly).

Same-zone trips have OSRM time 0 (both ends are the same representative point); the
OSRM-based baselines fall back to the train median for same-zone trips.
"""

import time

import lightgbm as lgb
import numpy as np
import polars as pl

from fleetline_ml.eta.dataset import WEATHER_COLS
from fleetline_ml.eta.metrics import report

FEATURES = [
    "pu_zone",
    "do_zone",
    "hour",
    "dow",
    "minute_of_day",
    "is_holiday",
    "osrm_s",
    "osrm_mi",
    *WEATHER_COLS,
]
CATEGORICAL = ["pu_zone", "do_zone"]


def _same_zone_fallback(train: pl.DataFrame) -> float:
    return float(train.filter(pl.col("osrm_s") <= 0)["trip_time_s"].median())


def b0_osrm(train: pl.DataFrame, test: pl.DataFrame) -> np.ndarray:
    fb = _same_zone_fallback(train)
    return np.where(test["osrm_s"] > 0, test["osrm_s"], fb)


def b1_global_factor(train: pl.DataFrame, test: pl.DataFrame) -> tuple[np.ndarray, float]:
    t = train.filter(pl.col("osrm_s") > 0)
    k = float((t["trip_time_s"] / t["osrm_s"]).median())
    fb = _same_zone_fallback(train)
    return np.where(test["osrm_s"] > 0, test["osrm_s"] * k, fb), k


def b2_hour_of_week(train: pl.DataFrame, test: pl.DataFrame) -> np.ndarray:
    how = (pl.col("dow").cast(pl.Int16) * 24 + pl.col("hour")).alias("how")
    factors = (
        train.filter(pl.col("osrm_s") > 0)
        .with_columns(how, ratio=pl.col("trip_time_s") / pl.col("osrm_s"))
        .group_by("how")
        .agg(pl.col("ratio").median().alias("k"))
    )
    fb = _same_zone_fallback(train)
    t = test.with_columns(how).join(factors, on="how", how="left")
    k = t["k"].fill_null(t["k"].median()).to_numpy()
    return np.where(t["osrm_s"] > 0, t["osrm_s"].to_numpy() * k, fb)


def b3_lightgbm(
    train: pl.DataFrame, val: pl.DataFrame, test: pl.DataFrame, seed: int = 7
) -> tuple[np.ndarray, dict]:
    # Plain float32 matrices; zone IDs are integers LightGBM treats as category IDs, so
    # train and test share one encoding (pandas categories can silently renumber).
    def xy(df: pl.DataFrame):
        x = df.select(FEATURES).cast(pl.Float32).to_numpy()
        return x, df["trip_time_s"].to_numpy()

    xtr, ytr = xy(train)
    xva, yva = xy(val)
    params = {
        "objective": "l1",
        "learning_rate": 0.1,
        "num_leaves": 255,
        "min_data_in_leaf": 200,
        "feature_fraction": 0.9,
        "bagging_fraction": 0.8,
        "bagging_freq": 1,
        "max_cat_to_onehot": 4,
        "cat_smooth": 10,
        "seed": seed,
        "verbose": -1,
    }
    t0 = time.time()
    model = lgb.train(
        params,
        lgb.Dataset(xtr, ytr, feature_name=FEATURES, categorical_feature=CATEGORICAL),
        num_boost_round=2000,
        valid_sets=[lgb.Dataset(xva, yva, feature_name=FEATURES, categorical_feature=CATEGORICAL)],
        callbacks=[lgb.early_stopping(50, verbose=False)],
    )
    xte, _ = xy(test)
    pred = model.predict(xte, num_iteration=model.best_iteration)
    info = {
        "best_iteration": model.best_iteration,
        "train_seconds": round(time.time() - t0, 1),
        "feature_importance_gain": dict(
            zip(FEATURES, map(float, model.feature_importance("gain")), strict=True)
        ),
    }
    return pred, info


def run_all(train: pl.DataFrame, val: pl.DataFrame, test: pl.DataFrame) -> dict:
    y = test["trip_time_s"].to_numpy()
    out = {"B0_osrm": report(y, b0_osrm(train, test))}
    p1, k = b1_global_factor(train, test)
    out["B1_osrm_x_global"] = {**report(y, p1), "factor": k}
    out["B2_osrm_x_hour_of_week"] = report(y, b2_hour_of_week(train, test))
    p3, info = b3_lightgbm(train, val, test)
    out["B3_lightgbm"] = {**report(y, p3), **info}
    return out
