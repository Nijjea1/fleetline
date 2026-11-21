import numpy as np

from fleetline_sim.calibrate import haversine_mi


def test_haversine_known_distance():
    # Times Square -> Empire State Building is ~0.66 mi as the crow flies.
    d = haversine_mi(
        np.array([40.7580]), np.array([-73.9855]), np.array([40.7484]), np.array([-73.9857])
    )
    assert abs(d[0] - 0.66) < 0.05


def test_haversine_zero():
    assert (
        haversine_mi(np.array([40.7]), np.array([-74.0]), np.array([40.7]), np.array([-74.0]))[0]
        == 0
    )
