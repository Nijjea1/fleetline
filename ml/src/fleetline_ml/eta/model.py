"""Residual ETA network (DeepETA-style).

prediction = base + MLP(features)

`base` is the B2 routing baseline (OSRM free-flow time x hour-of-week traffic factor). The
network only learns what the routing engine gets wrong, which is a much easier target than
the full travel time and keeps the model's output sensible even early in training.

Inputs:
  - embeddings: pickup zone, dropoff zone, hour of week, and a hashed (pickup, dropoff)
    pair. 69k zone pairs is too many for one table per pair to train well, so pairs share
    2^17 buckets (feature hashing, as in DeepETA's feature crosses).
  - continuous: log OSRM time and distance, time of day as sin/cos (so 23:59 is next to
    00:00), holiday flag, and standardized weather.
"""

import math

import torch
from torch import nn

N_ZONES = 264  # zone IDs are 1..263; 0 unused
N_HOW = 168  # hours in a week
PAIR_BUCKETS = 1 << 17  # > 69k pairs, so most pairs get their own slot
N_CONT = 10


def pair_bucket(pu: torch.Tensor, do: torch.Tensor) -> torch.Tensor:
    # A fixed multiplicative hash (Knuth) of the pair, folded into PAIR_BUCKETS.
    return ((pu.long() * 2654435761 + do.long()) % PAIR_BUCKETS).long()


class ResidualETA(nn.Module):
    def __init__(
        self,
        zone_dim: int = 16,
        how_dim: int = 8,
        pair_dim: int = 16,
        hidden: int = 256,
        dropout: float = 0.05,
    ):
        super().__init__()
        self.pu = nn.Embedding(N_ZONES, zone_dim)
        self.do = nn.Embedding(N_ZONES, zone_dim)
        self.how = nn.Embedding(N_HOW, how_dim)
        self.pair = nn.Embedding(PAIR_BUCKETS, pair_dim)
        d = 2 * zone_dim + how_dim + pair_dim + N_CONT
        self.mlp = nn.Sequential(
            nn.Linear(d, hidden),
            nn.ReLU(),
            nn.Dropout(dropout),
            nn.Linear(hidden, hidden),
            nn.ReLU(),
            nn.Dropout(dropout),
            nn.Linear(hidden, hidden // 2),
            nn.ReLU(),
            nn.Linear(hidden // 2, 1),
        )
        # Start as "no correction": the untrained model predicts exactly the base.
        nn.init.zeros_(self.mlp[-1].weight)
        nn.init.zeros_(self.mlp[-1].bias)

    def forward(
        self, zones: torch.Tensor, how: torch.Tensor, cont: torch.Tensor, base: torch.Tensor
    ) -> torch.Tensor:
        pu, do = zones[:, 0], zones[:, 1]
        x = torch.cat(
            [self.pu(pu), self.do(do), self.how(how), self.pair(pair_bucket(pu, do)), cont], dim=1
        )
        # Residual is predicted in units of 100 s to keep activations O(1).
        return base + 100.0 * self.mlp(x).squeeze(1)


def time_of_day_features(minute_of_day: torch.Tensor) -> tuple[torch.Tensor, torch.Tensor]:
    a = 2 * math.pi * minute_of_day.float() / 1440
    return torch.sin(a), torch.cos(a)
