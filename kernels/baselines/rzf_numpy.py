#!/usr/bin/env python3
"""NumPy RZF baseline (CPU). Same math as the oracle, configurable precision.

This is the portable cross-stack baseline for kernels/host/compare.py. It uses
an LU solve (`np.linalg.solve`) rather than forming an explicit inverse, so it
honors the same "no explicit inverse" rule as the GPU kernels while staying
vectorized and fast enough to benchmark. Precision is selectable so a complex64
baseline can be compared against the FP32 GPU kernel under matched rules.
"""
from __future__ import annotations

import numpy as np


def rzf_numpy(channel: np.ndarray, alpha: float, dtype=np.complex64) -> np.ndarray:
    h = np.asarray(channel, dtype=dtype)
    if h.ndim != 3:
        raise ValueError("channel must be [batch, users, antennas]")
    batch, users, antennas = h.shape
    if batch <= 0 or users <= 0 or antennas < users or not (alpha > 0):
        raise ValueError("require batch/users>0, antennas>=users, alpha>0")
    real_dt = np.empty((), dtype=dtype).real.dtype
    gram = h @ np.conj(np.swapaxes(h, -1, -2))              # [B,U,U] = H H^H
    reg = gram + (real_dt.type(alpha) * np.eye(users, dtype=dtype))[None, :, :]
    solved = np.linalg.solve(reg, h)                        # A X = H, no inverse
    w = np.conj(np.swapaxes(solved, -1, -2)).astype(dtype)  # [B,N,U]
    norm = np.linalg.norm(w, axis=(-2, -1), keepdims=True)
    tiny = np.finfo(real_dt).tiny
    return (w / np.maximum(norm, tiny)).astype(dtype)


if __name__ == "__main__":
    import json
    import sys
    sys.path.insert(0, __file__.rsplit("/", 3)[0] + "/kernels/rzf")
    from rzf_reference import metrics, rzf_precoder  # type: ignore

    rng = np.random.default_rng(7)
    h = (rng.normal(size=(4, 8, 32)) + 1j * rng.normal(size=(4, 8, 32))) / np.sqrt(2)
    ref = rzf_precoder(h, 0.1, np.complex128)
    cand = rzf_numpy(h, 0.1, np.complex64)
    print(json.dumps({"numpyBaselineVsOracle": metrics(ref, cand, h)}, indent=2))
