import math
import sys
import unittest
from pathlib import Path
import numpy as np

# Keep the test directly runnable from either the repository root or this
# directory. Production code does not rely on this path adjustment.
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from c2k_stats import (
    binomial_cdf,
    conformal_rank,
    fit_mahalanobis,
    fit_ridge,
    split_conformal_upper,
    tolerance_rank,
)

class StatisticsTest(unittest.TestCase):
    def test_conformal_rank(self):
        self.assertEqual(conformal_rank(99, 0.95), 95)
        with self.assertRaises(ValueError):
            conformal_rank(10, 0.99)

    def test_tolerance_rank_meets_definition(self):
        k = tolerance_rank(400, 0.99, 0.95)
        self.assertGreaterEqual(binomial_cdf(k - 1, 400, 0.99), 0.95)
        if k > 1:
            self.assertLess(binomial_cdf(k - 2, 400, 0.99), 0.95)

    def test_ridge_original_units(self):
        rng = np.random.default_rng(3)
        x = rng.normal(size=(500, 3)) * np.array([1.0, 10.0, 100.0])
        y = 7.0 + x @ np.array([2.0, -0.5, 0.03])
        m = fit_ridge(x, y, 1e-8)
        self.assertTrue(np.allclose(m.coefficients, [2.0, -0.5, 0.03], atol=1e-7))
        self.assertAlmostEqual(m.intercept, 7.0, places=6)

    def test_mahalanobis_is_finite(self):
        rng = np.random.default_rng(4)
        x = rng.normal(size=(200, 4))
        m = fit_mahalanobis(x, 1e-4)
        scores = m.score(x[:10])
        self.assertTrue(np.isfinite(scores).all())
        self.assertTrue((scores >= -1e-10).all())

    def test_upper_order_statistic(self):
        value, rank = split_conformal_upper(range(1, 100), 0.95)
        self.assertEqual(rank, 95)
        self.assertEqual(value, 95.0)

if __name__ == "__main__":
    unittest.main()
