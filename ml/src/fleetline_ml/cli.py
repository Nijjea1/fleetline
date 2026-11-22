"""ML command line: `uv run fleetline-ml --help`."""

import typer

app = typer.Typer(help="Fleetline ML", no_args_is_help=True)


@app.callback()
def main() -> None:
    """ETA model pipeline (more commands are added per step)."""


@app.command("osrm-matrix")
def osrm_matrix() -> None:
    """Build the 263x263 zone-to-zone free-flow time matrix from local OSRM."""
    from fleetline_ml.eta import osrm_matrix as om

    df = om.build()
    missing = df.filter(df["osrm_s"].is_nan()).height
    print(f"{df.height:,} zone pairs ({missing} unroutable) -> {om.write(df)}")
    print(df.describe())


@app.command()
def weather(start: str = "2026-09-01", end: str = "2026-09-30") -> None:
    """Download hourly Central Park weather for a date range."""
    from fleetline_ml.eta import weather as w

    df = w.fetch(start, end)
    rain = (df["precipitation"] > 0).sum()
    print(f"{df.height} hours, rain hours: {rain} -> {w.write(df, start, end)}")


@app.command()
def dataset(month: str = "2026-09") -> None:
    """Build train/val/test parquet files for the ETA model (split by date)."""
    from fleetline_ml.eta import dataset as ds

    for name, info in ds.build(month).items():
        print(f"{name:5s} {info['rows']:>11,} rows over {info['days']} days")


@app.command()
def baselines() -> None:
    """E5 part 1: evaluate B0-B3 on the test days; write ml/results/e5_baselines.json."""
    import json

    import polars as pl

    from fleetline_ml.eta import baselines as b
    from fleetline_ml.paths import ETA_DIR, RESULTS_DIR

    train, val, test = (pl.read_parquet(ETA_DIR / f"{s}.parquet") for s in ("train", "val", "test"))
    res = b.run_all(train, val, test)
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    (RESULTS_DIR / "e5_baselines.json").write_text(json.dumps(res, indent=2) + "\n")
    for name, r in res.items():
        print(
            f"{name:24s} MAE {r['mae_s']:7.1f}s  MAPE {r['mape']:.3f}  "
            f"p90 {r['p90_abs_err_s']:7.1f}s  bias {r['bias_s']:+.1f}s"
        )
