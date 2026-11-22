"""Train the residual ETA model and evaluate it on the held-out test days (E5).

Everything the model needs at inference time (the baseline's hour-of-week factors,
feature normalization) is fitted on the training split only and saved with the weights.
"""

import json
import time
from dataclasses import asdict, dataclass

import numpy as np
import polars as pl
import torch

from fleetline_ml.eta.dataset import WEATHER_COLS
from fleetline_ml.eta.metrics import report
from fleetline_ml.eta.model import ResidualETA, time_of_day_features
from fleetline_ml.paths import ETA_DIR, MODELS_DIR, RESULTS_DIR


@dataclass
class TrainConfig:
    epochs: int = 4
    batch: int = 8192
    lr: float = 2e-3
    weight_decay: float = 1e-5
    seed: int = 7
    max_train_rows: int = 0  # 0 = all
    evals_per_epoch: int = 4


@dataclass
class Prep:
    """Statistics fitted on train and reused for val/test/inference."""

    factors: list[float]  # 168 hour-of-week traffic factors (the B2 baseline)
    same_zone_s: float  # baseline for same-zone trips (OSRM time is 0)
    w_mean: list[float]
    w_std: list[float]


def fit_prep(train: pl.DataFrame) -> Prep:
    t = train.filter(pl.col("osrm_s") > 0).with_columns(
        how=pl.col("dow").cast(pl.Int16) * 24 + pl.col("hour"),
        ratio=pl.col("trip_time_s") / pl.col("osrm_s"),
    )
    f = t.group_by("how").agg(pl.col("ratio").median()).sort("how")
    factors = np.full(168, float(t["ratio"].median()))
    factors[f["how"].to_numpy()] = f["ratio"].to_numpy()
    w = train.select(WEATHER_COLS).to_numpy()
    return Prep(
        factors=factors.tolist(),
        same_zone_s=float(train.filter(pl.col("osrm_s") <= 0)["trip_time_s"].median()),
        w_mean=w.mean(0).tolist(),
        w_std=(w.std(0) + 1e-6).tolist(),
    )


def tensors(df: pl.DataFrame, p: Prep, device: str) -> dict[str, torch.Tensor]:
    how = (df["dow"].cast(pl.Int16) * 24 + df["hour"].cast(pl.Int16)).to_numpy()
    osrm = df["osrm_s"].to_numpy()
    base = np.where(osrm > 0, osrm * np.asarray(p.factors)[how], p.same_zone_s)
    sin, cos = time_of_day_features(torch.from_numpy(df["minute_of_day"].to_numpy()))
    w = (df.select(WEATHER_COLS).to_numpy() - np.asarray(p.w_mean)) / np.asarray(p.w_std)
    cont = np.column_stack(
        [
            np.log1p(osrm),
            np.log1p(df["osrm_mi"].to_numpy()),
            sin.numpy(),
            cos.numpy(),
            df["is_holiday"].cast(pl.Float32).to_numpy(),
            (osrm <= 0).astype(np.float32),
            w,
        ]
    ).astype(np.float32)
    out = {
        "zones": torch.from_numpy(df.select("pu_zone", "do_zone").to_numpy().astype(np.int64)),
        "how": torch.from_numpy(how.astype(np.int64)),
        "cont": torch.from_numpy(cont),
        "base": torch.from_numpy(base.astype(np.float32)),
        "y": torch.from_numpy(df["trip_time_s"].to_numpy().astype(np.float32)),
    }
    return {k: v.to(device) for k, v in out.items()}


@torch.no_grad()
def predict(model: ResidualETA, t: dict[str, torch.Tensor], batch: int = 65536) -> np.ndarray:
    model.eval()
    out = []
    for i in range(0, len(t["y"]), batch):
        s = slice(i, i + batch)
        out.append(model(t["zones"][s], t["how"][s], t["cont"][s], t["base"][s]).float().cpu())
    return torch.cat(out).numpy()


def device() -> str:
    if torch.cuda.is_available():
        return "cuda"
    if torch.backends.mps.is_available():
        return "mps"
    return "cpu"


def train(cfg: TrainConfig) -> dict:
    torch.manual_seed(cfg.seed)
    dev = device()
    train_df = pl.read_parquet(ETA_DIR / "train.parquet")
    if cfg.max_train_rows:
        train_df = train_df.sample(cfg.max_train_rows, seed=cfg.seed)
    val_df = pl.read_parquet(ETA_DIR / "val.parquet")
    test_df = pl.read_parquet(ETA_DIR / "test.parquet")
    prep = fit_prep(train_df)
    tr, va, te = (tensors(d, prep, dev) for d in (train_df, val_df, test_df))

    model = ResidualETA().to(dev)
    opt = torch.optim.AdamW(model.parameters(), lr=cfg.lr, weight_decay=cfg.weight_decay)
    n = len(tr["y"])
    steps_per_epoch = n // cfg.batch
    sched = torch.optim.lr_scheduler.OneCycleLR(
        opt, max_lr=cfg.lr, total_steps=cfg.epochs * steps_per_epoch
    )
    eval_every = max(1, steps_per_epoch // cfg.evals_per_epoch)
    y_val = va["y"].cpu().numpy()
    best = {"mae": float("inf"), "state": None, "step": 0}
    history = []
    t0 = time.time()
    step = 0
    gen = torch.Generator(device="cpu").manual_seed(cfg.seed)
    for epoch in range(cfg.epochs):
        perm = torch.randperm(n, generator=gen).to(dev)
        model.train()
        for i in range(steps_per_epoch):
            idx = perm[i * cfg.batch : (i + 1) * cfg.batch]
            pred = model(tr["zones"][idx], tr["how"][idx], tr["cont"][idx], tr["base"][idx])
            loss = (pred - tr["y"][idx]).abs().mean()  # L1 = optimize MAE directly
            opt.zero_grad(set_to_none=True)
            loss.backward()
            opt.step()
            sched.step()
            step += 1
            if step % eval_every == 0:
                mae = float(np.abs(predict(model, va) - y_val).mean())
                history.append(
                    {
                        "step": step,
                        "epoch": epoch,
                        "train_l1": float(loss),
                        "val_mae": mae,
                        "elapsed_s": round(time.time() - t0, 1),
                    }
                )
                print(
                    f"epoch {epoch} step {step:6d}  train L1 {float(loss):6.1f}  val MAE {mae:6.1f}"
                )
                if mae < best["mae"]:
                    best = {
                        "mae": mae,
                        "step": step,
                        "state": {
                            k: v.detach().cpu().clone() for k, v in model.state_dict().items()
                        },
                    }
                model.train()

    model.load_state_dict(best["state"])
    y_test = te["y"].cpu().numpy()
    result = {
        "model": "residual_mlp",
        "device": dev,
        "params": sum(p.numel() for p in model.parameters()),
        "config": asdict(cfg),
        "best_step": best["step"],
        "val_mae_s": best["mae"],
        "train_seconds": round(time.time() - t0, 1),
        "test": report(y_test, predict(model, te)),
        "test_base_B2": report(y_test, te["base"].cpu().numpy()),
        "history": history,
    }
    MODELS_DIR.mkdir(parents=True, exist_ok=True)
    torch.save({"state_dict": best["state"], "prep": asdict(prep)}, MODELS_DIR / "eta_residual.pt")
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    (RESULTS_DIR / "e5_residual_mlp.json").write_text(json.dumps(result, indent=2) + "\n")
    return result
