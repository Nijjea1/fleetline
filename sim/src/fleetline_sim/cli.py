"""Command-line entry point: `uv run fleetline-sim --help`."""

import typer

from fleetline_sim import download as dl

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
