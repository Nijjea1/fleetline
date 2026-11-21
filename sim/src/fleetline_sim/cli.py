"""Command-line entry point: `uv run fleetline-sim --help`."""

from datetime import date

import typer

from fleetline_sim import download as dl
from fleetline_sim import prepare as prep
from fleetline_sim.paths import RAW_DIR

app = typer.Typer(help="Fleetline demand simulator", no_args_is_help=True)

DEFAULT_MONTH = "2026-09"


@app.callback()
def main() -> None:
    """Keep subcommands explicit even while there is only one."""


@app.command()
def download(
    month: str = typer.Option(DEFAULT_MONTH, help="HVFHV month to fetch, YYYY-MM"),
    force: bool = typer.Option(False, help="Re-download even if the file exists"),
) -> None:
    """Download one month of Uber/Lyft trips plus the taxi zone shapefile."""
    dl.download_zones(force=force)
    dl.download_month(month, force=force)


@app.command()
def prepare(
    day: str = typer.Option("2026-09-16", help="Replay day, YYYY-MM-DD"),
    seed: int = typer.Option(7, help="Seed for point-in-zone sampling"),
) -> None:
    """Clean one day of trips and geocode pickups/dropoffs into a replay file."""
    d = date.fromisoformat(day)
    prep.build_day(prep.month_file_for(d), RAW_DIR / "taxi_zones.zip", d, seed)
