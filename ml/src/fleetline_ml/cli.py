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
    print(f"{df.height} hours, rain hours: {(df['precipitation'] > 0).sum()} -> {w.write(df, start, end)}")
