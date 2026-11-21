"""Where data lives on disk. Everything goes under <repo>/data, which is gitignored."""

from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[3]
DATA_DIR = REPO_ROOT / "data"
RAW_DIR = DATA_DIR / "raw"          # untouched downloads
DERIVED_DIR = DATA_DIR / "derived"  # files our scripts build from raw
