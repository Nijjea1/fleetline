import numpy as np
import shapely

from fleetline_sim.zones import sample_points_in_polygon

# An L-shaped polygon: only 3/4 of its bounding box is inside, so rejection matters.
L_SHAPE = shapely.Polygon([(0, 0), (2, 0), (2, 1), (1, 1), (1, 2), (0, 2)])


def test_all_points_inside():
    x, y = sample_points_in_polygon(L_SHAPE, 5_000, np.random.default_rng(0))
    assert len(x) == 5_000
    assert shapely.contains_xy(L_SHAPE, x, y).all()


def test_same_seed_same_points():
    a = sample_points_in_polygon(L_SHAPE, 100, np.random.default_rng(42))
    b = sample_points_in_polygon(L_SHAPE, 100, np.random.default_rng(42))
    assert np.array_equal(a[0], b[0]) and np.array_equal(a[1], b[1])


def test_roughly_uniform():
    # The missing top-right square should get nothing; each of the 3 filled squares ~1/3.
    x, y = sample_points_in_polygon(L_SHAPE, 30_000, np.random.default_rng(1))
    bottom_left = ((x < 1) & (y < 1)).mean()
    assert abs(bottom_left - 1 / 3) < 0.02


def test_zero_points():
    x, y = sample_points_in_polygon(L_SHAPE, 0, np.random.default_rng(0))
    assert len(x) == 0
