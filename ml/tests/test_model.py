import torch

from fleetline_ml.eta.model import N_CONT, ResidualETA, pair_bucket


def batch(n=32):
    zones = torch.randint(1, 264, (n, 2))
    how = torch.randint(0, 168, (n,))
    cont = torch.randn(n, N_CONT)
    base = torch.rand(n) * 2000 + 100
    return zones, how, cont, base


def test_untrained_model_predicts_base():
    m = ResidualETA().eval()
    zones, how, cont, base = batch()
    assert torch.allclose(m(zones, how, cont, base), base)


def test_output_shape_and_grad():
    m = ResidualETA()
    zones, how, cont, base = batch(64)
    out = m(zones, how, cont, base)
    assert out.shape == (64,)
    out.abs().mean().backward()
    assert m.mlp[-1].weight.grad is not None


def test_pair_bucket_is_directional_and_in_range():
    a = pair_bucket(torch.tensor([1]), torch.tensor([2]))
    b = pair_bucket(torch.tensor([2]), torch.tensor([1]))
    assert a.item() != b.item()  # A->B traffic differs from B->A
    allb = pair_bucket(
        torch.arange(1, 264).repeat_interleave(263), torch.arange(1, 264).repeat(263)
    )
    assert allb.min() >= 0 and allb.max() < 1 << 17
