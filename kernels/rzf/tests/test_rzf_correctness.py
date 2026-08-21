#!/usr/bin/env python3
"""RZF correctness tests: numerical, boundary, and fail-closed semantics.

Every test runs against the CPU spec (`rzf_algorithm.rzf_precoder_cholesky`,
which matches the complex128 oracle to ~1e-16). When a c2k_rzf backend shared
library is present (CUDA or Mojo), the SAME cases are replayed on device and the
per-batch status + numerical agreement are asserted to match -- so the GPU
kernel's fail-closed behavior is verified, not assumed.

Run:
  PYTHONPATH=kernels/rzf:kernels/host \
    python -m unittest discover -s kernels/rzf/tests -v
Point at a backend explicitly with C2K_RZF_LIB=/path/to/libc2k_rzf*.so.

Coverage: zero matrix, nearly singular (regularization), large dynamic range,
NaN/Inf, U>N, boundary shapes, odd dimensions, determinism, complex64 accuracy.
"""
from __future__ import annotations

import unittest

import numpy as np

from rzf_algorithm import (
    STATUS_NON_FINITE_INPUT,
    STATUS_NOT_POSITIVE_DEFINITE,
    STATUS_OK,
    rzf_precoder_cholesky,
)
from rzf_reference import metrics, rzf_precoder

try:  # optional device backend
    from rzf_host import open_backend
except Exception:  # pragma: no cover
    open_backend = None  # type: ignore


def _rand_channel(batch, users, antennas, seed):
    rng = np.random.default_rng(seed)
    return (rng.normal(size=(batch, users, antennas))
            + 1j * rng.normal(size=(batch, users, antennas))) / np.sqrt(2)


class _BackendMixin:
    """Opens a device backend once for the whole class (or records absence)."""

    @classmethod
    def backend(cls, batch, users, antennas):
        # open_backend returns None only when NO backend library is present -- a
        # legitimate skip. If a library IS present but create() fails, let the
        # exception propagate: a broken backend must fail the test, not skip it.
        if open_backend is None:
            return None
        return open_backend(max(batch, 1), max(users, 1), max(antennas, 1))


class TestNumericalAgreement(unittest.TestCase, _BackendMixin):
    def test_complex64_matches_complex128_oracle(self):
        h = _rand_channel(4, 8, 32, 7)
        ref = rzf_precoder(h, 0.1, np.complex128)
        w, status = rzf_precoder_cholesky(h, 0.1, np.complex64)
        self.assertTrue((status == STATUS_OK).all())
        self.assertLess(metrics(ref, w, h)["relativeL2"], 1e-4)

    def test_complex128_mirror_matches_oracle_to_machine_precision(self):
        h = _rand_channel(3, 5, 9, 11)
        ref = rzf_precoder(h, 0.25, np.complex128)
        w, status = rzf_precoder_cholesky(h, 0.25, np.complex128)
        self.assertTrue((status == STATUS_OK).all())
        self.assertLess(metrics(ref, w, h)["relativeL2"], 1e-10)

    def test_gpu_backend_matches_oracle_when_present(self):
        be = self.backend(4, 8, 32)
        if be is None:
            self.skipTest("NOT RUN: no c2k_rzf backend library present")
        with be:
            h = _rand_channel(4, 8, 32, 7)
            ref = rzf_precoder(h, 0.1, np.complex128)
            w, status, _ = be.run(h, 0.1)
            self.assertTrue((status == STATUS_OK).all())
            self.assertLess(metrics(ref, w, h)["relativeL2"], 1e-4)


class TestZeroMatrix(unittest.TestCase, _BackendMixin):
    def test_zero_channel_returns_zero_precoder(self):
        h = np.zeros((2, 4, 8), dtype=np.complex128)
        w, status = rzf_precoder_cholesky(h, 0.1, np.complex64)
        self.assertTrue((status == STATUS_OK).all())
        self.assertTrue(np.all(w == 0))
        # Oracle agrees: reg = alpha I, solve -> 0, norm 0 -> divide by tiny -> 0.
        self.assertTrue(np.all(rzf_precoder(h, 0.1, np.complex128) == 0))

    def test_gpu_zero_channel(self):
        be = self.backend(2, 4, 8)
        if be is None:
            self.skipTest("NOT RUN: no c2k_rzf backend library present")
        with be:
            w, status, _ = be.run(np.zeros((2, 4, 8), dtype=np.complex64), 0.1)
            self.assertTrue((status == STATUS_OK).all())
            self.assertTrue(np.all(w == 0))


class TestNearlySingular(unittest.TestCase, _BackendMixin):
    """Regularization semantics: a rank-deficient H makes H H^H singular, but
    alpha>0 makes H H^H + alpha I positive-definite and solvable."""

    def _rank_deficient(self):
        h = _rand_channel(1, 4, 16, 3).astype(np.complex128)
        h[0, 1, :] = h[0, 0, :]      # duplicate user rows -> rank deficient
        h[0, 3, :] = h[0, 2, :]
        return h

    def test_regularization_makes_it_solvable(self):
        h = self._rank_deficient()
        w, status = rzf_precoder_cholesky(h, 0.1, np.complex64)
        self.assertEqual(status[0], STATUS_OK)
        self.assertTrue(np.isfinite(w).all())
        ref = rzf_precoder(h, 0.1, np.complex128)
        self.assertLess(metrics(ref, w, h)["relativeL2"], 1e-3)

    def test_gpu_regularization(self):
        be = self.backend(1, 4, 16)
        if be is None:
            self.skipTest("NOT RUN: no c2k_rzf backend library present")
        with be:
            h = self._rank_deficient()
            w, status, _ = be.run(h, 0.1)
            self.assertEqual(status[0], STATUS_OK)
            self.assertTrue(np.isfinite(w).all())


class TestLargeDynamicRange(unittest.TestCase, _BackendMixin):
    def test_moderate_dynamic_range_matches_oracle(self):
        h = _rand_channel(2, 4, 16, 5).astype(np.complex128)
        h[0] *= 1e3
        h[1] *= 1e-3
        w, status = rzf_precoder_cholesky(h, 0.1, np.complex64)
        self.assertTrue((status == STATUS_OK).all())
        self.assertTrue(np.isfinite(w).all())
        ref = rzf_precoder(h, 0.1, np.complex128)
        self.assertLess(metrics(ref, w, h)["relativeL2"], 1e-2)

    def test_extreme_range_is_fail_closed_never_nan(self):
        # fp32 Gram overflows; the kernel must flag it, not emit NaN/Inf garbage.
        h = (_rand_channel(1, 4, 16, 9) * 1e20).astype(np.complex128)
        w, status = rzf_precoder_cholesky(h, 0.1, np.complex64)
        self.assertIn(status[0], (STATUS_NOT_POSITIVE_DEFINITE, STATUS_NON_FINITE_INPUT))
        self.assertTrue(np.all(w[0] == 0))
        self.assertTrue(np.isfinite(w).all())

    def test_gpu_extreme_range_fail_closed(self):
        be = self.backend(1, 4, 16)
        if be is None:
            self.skipTest("NOT RUN: no c2k_rzf backend library present")
        with be:
            h = (_rand_channel(1, 4, 16, 9) * 1e20).astype(np.complex64)
            w, status, _ = be.run(h, 0.1)
            self.assertIn(status[0], (STATUS_NOT_POSITIVE_DEFINITE, STATUS_NON_FINITE_INPUT))
            self.assertTrue(np.all(w[0] == 0))
            self.assertTrue(np.isfinite(w).all())


class TestNaNInf(unittest.TestCase, _BackendMixin):
    def _mixed(self):
        h = _rand_channel(3, 4, 8, 13).astype(np.complex64)
        h[0, 0, 0] = np.nan
        h[1, 2, 3] = np.inf + 0j
        # h[2] stays finite and must succeed.
        return h

    def test_non_finite_is_flagged_and_zeroed(self):
        h = self._mixed()
        w, status = rzf_precoder_cholesky(h, 0.1, np.complex64)
        self.assertEqual(status[0], STATUS_NON_FINITE_INPUT)
        self.assertEqual(status[1], STATUS_NON_FINITE_INPUT)
        self.assertEqual(status[2], STATUS_OK)
        self.assertTrue(np.all(w[0] == 0) and np.all(w[1] == 0))
        self.assertTrue(np.isfinite(w).all())  # no NaN/Inf ever leaks

    def test_gpu_non_finite(self):
        be = self.backend(3, 4, 8)
        if be is None:
            self.skipTest("NOT RUN: no c2k_rzf backend library present")
        with be:
            w, status, _ = be.run(self._mixed(), 0.1)
            self.assertEqual(status[0], STATUS_NON_FINITE_INPUT)
            self.assertEqual(status[1], STATUS_NON_FINITE_INPUT)
            self.assertEqual(status[2], STATUS_OK)
            self.assertTrue(np.isfinite(w).all())


class TestUGreaterThanN(unittest.TestCase, _BackendMixin):
    def test_u_gt_n_rejected_by_cpu_spec(self):
        h = _rand_channel(1, 8, 4, 1)  # users=8 > antennas=4
        with self.assertRaises(ValueError):
            rzf_precoder_cholesky(h, 0.1, np.complex64)
        with self.assertRaises(ValueError):
            rzf_precoder(h, 0.1, np.complex128)  # oracle rejects too

    def test_u_gt_n_rejected_by_gpu_host(self):
        if open_backend is None:
            self.skipTest("NOT RUN: no c2k_rzf backend library present")
        be = open_backend(1, 8, 8)
        if be is None:
            self.skipTest("NOT RUN: no c2k_rzf backend library present")
        with be:
            with self.assertRaises(ValueError):
                be.run(_rand_channel(1, 8, 4, 1), 0.1)


class TestBoundaryShapes(unittest.TestCase, _BackendMixin):
    def test_square_u_equals_n(self):
        self._check(1, 8, 8, 21)

    def test_single_user(self):
        self._check(2, 1, 4, 22)

    def test_single_user_single_antenna(self):
        self._check(1, 1, 1, 23)

    def test_single_batch_wide(self):
        self._check(1, 2, 64, 24)

    def _check(self, b, u, n, seed):
        h = _rand_channel(b, u, n, seed)
        ref = rzf_precoder(h, 0.1, np.complex128)
        w, status = rzf_precoder_cholesky(h, 0.1, np.complex64)
        self.assertTrue((status == STATUS_OK).all())
        self.assertEqual(w.shape, (b, n, u))
        self.assertLess(metrics(ref, w, h)["relativeL2"], 1e-3)
        be = self.backend(b, u, n)
        if be is not None:
            with be:
                wg, sg, _ = be.run(h, 0.1)
                self.assertTrue((sg == STATUS_OK).all())
                self.assertLess(metrics(ref, wg, h)["relativeL2"], 1e-3)


class TestOddDimensions(unittest.TestCase, _BackendMixin):
    def test_odd_shapes(self):
        for (b, u, n, seed) in [(3, 3, 5, 31), (5, 7, 13, 32), (1, 3, 7, 33), (2, 5, 5, 34)]:
            with self.subTest(shape=(b, u, n)):
                h = _rand_channel(b, u, n, seed)
                ref = rzf_precoder(h, 0.1, np.complex128)
                w, status = rzf_precoder_cholesky(h, 0.1, np.complex64)
                self.assertTrue((status == STATUS_OK).all())
                self.assertLess(metrics(ref, w, h)["relativeL2"], 1e-3)
                be = self.backend(b, u, n)
                if be is not None:
                    with be:
                        wg, sg, _ = be.run(h, 0.1)
                        self.assertTrue((sg == STATUS_OK).all())
                        self.assertLess(metrics(ref, wg, h)["relativeL2"], 1e-3)


class TestDeterminism(unittest.TestCase, _BackendMixin):
    def test_cpu_bitwise_repeatable(self):
        h = _rand_channel(4, 6, 20, 41)
        w1, _ = rzf_precoder_cholesky(h, 0.1, np.complex64, deterministic=True)
        w2, _ = rzf_precoder_cholesky(h, 0.1, np.complex64, deterministic=True)
        self.assertTrue(np.array_equal(w1.view(np.float32), w2.view(np.float32)))

    def test_gpu_bitwise_repeatable(self):
        be = self.backend(4, 6, 20)
        if be is None:
            self.skipTest("NOT RUN: no c2k_rzf backend library present")
        with be:
            h = _rand_channel(4, 6, 20, 41)
            w1, _, _ = be.run(h, 0.1, deterministic=True)
            w2, _, _ = be.run(h, 0.1, deterministic=True)
            self.assertTrue(np.array_equal(w1.view(np.float32), w2.view(np.float32)))


if __name__ == "__main__":
    unittest.main(verbosity=2)
