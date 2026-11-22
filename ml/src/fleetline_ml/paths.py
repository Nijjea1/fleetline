"""Shared paths. All data lives under <repo>/data (gitignored)."""

from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[3]
DATA_DIR = REPO_ROOT / "data"
RAW_DIR = DATA_DIR / "raw"
DERIVED_DIR = DATA_DIR / "derived"
ETA_DIR = DERIVED_DIR / "eta"
RESULTS_DIR = REPO_ROOT / "ml" / "results"
MODELS_DIR = DATA_DIR / "models"
