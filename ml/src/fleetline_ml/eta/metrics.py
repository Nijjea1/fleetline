"""ETA error metrics. MAE (seconds) is the headline, as in the scope doc (E5)."""

import numpy as np


def report(y: np.ndarray, yhat: np.ndarray) -> dict[str, float]:
    err = yhat - y
    ae = np.abs(err)
    return {
        "mae_s": float(ae.mean()),
        "mape": float((ae / y).mean()),
        "p50_abs_err_s": float(np.quantile(ae, 0.5)),
        "p90_abs_err_s": float(np.quantile(ae, 0.9)),
        "rmse_s": float(np.sqrt((err**2).mean())),
        "bias_s": float(err.mean()),  # > 0 means we predict too long on average
        "n": int(len(y)),
    }
