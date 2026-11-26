"""The challenge's official scorer, used unmodified.

score.py (MIT-CAVE/rc-cli) carries no license file, so it isn't vendored: it's downloaded
on first use into data/raw/almrrc/ and imported from there. Lower is better; 0 = the
driver's exact order. Route score = sequence deviation x ERP-per-edit on the normalized
travel-time matrix.
"""

import importlib.util
import urllib.request

from .data import RAW, Route

URL = "https://raw.githubusercontent.com/MIT-CAVE/rc-cli/main/scoring/score.py"
_mod = None


def _score_module():
    global _mod
    if _mod is None:
        path = RAW / "score.py"
        if not path.exists():
            urllib.request.urlretrieve(URL, path)
        spec = importlib.util.spec_from_file_location("almrrc_score", path)
        _mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(_mod)
    return _mod


def route_score(r: Route, order: list[int]) -> float:
    """Official score of `order` against the driver's actual order for one route."""
    m = _score_module()
    actual = [r.stops[i] for i in r.actual] + [r.stops[0]]
    sub = [r.stops[i] for i in order] + [r.stops[0]]
    if m.isinvalid(actual, sub):
        raise ValueError(f"{r.id}: invalid sequence")
    cost = {a: {b: float(r.tt[i, j]) for j, b in enumerate(r.stops)} for i, a in enumerate(r.stops)}
    return float(m.score(actual, sub, cost))
