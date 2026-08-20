#!/usr/bin/env python3
"""Complex-valued regularized zero-forcing (RZF) correctness oracle."""
from __future__ import annotations
import argparse, json
import numpy as np


def rzf_precoder(channel: np.ndarray, alpha: float, dtype=np.complex128) -> np.ndarray:
    h = np.asarray(channel, dtype=dtype)
    if h.ndim != 3:
        raise ValueError("channel must have shape [batch, users, antennas]")
    batch, users, antennas = h.shape
    if batch <= 0 or users <= 0 or antennas < users or alpha <= 0:
        raise ValueError("require batch/users > 0, antennas >= users, alpha > 0")
    gram = h @ np.swapaxes(h.conj(), -1, -2)
    reg = gram + alpha * np.eye(users, dtype=dtype)[None, :, :]
    solved = np.linalg.solve(reg, h)
    w = np.swapaxes(solved.conj(), -1, -2)
    norm = np.linalg.norm(w, axis=(-2, -1), keepdims=True)
    return w / np.maximum(norm, np.finfo(np.empty((), dtype=dtype).real.dtype).tiny)


def metrics(reference: np.ndarray, candidate: np.ndarray, channel: np.ndarray) -> dict[str, float]:
    ref = np.asarray(reference, dtype=np.complex128)
    cand = np.asarray(candidate, dtype=np.complex128)
    rel = float(np.linalg.norm(cand - ref) / max(np.linalg.norm(ref), np.finfo(float).tiny))
    max_abs = float(np.max(np.abs(cand - ref)))
    h = np.asarray(channel, dtype=np.complex128)
    ref_eff = h @ ref
    cand_eff = h @ cand
    ref_signal = np.mean(np.abs(np.diagonal(ref_eff, axis1=-2, axis2=-1)) ** 2)
    cand_signal = np.mean(np.abs(np.diagonal(cand_eff, axis1=-2, axis2=-1)) ** 2)
    beamforming_loss_db = float(10 * np.log10(max(ref_signal, 1e-300) / max(cand_signal, 1e-300)))
    return {"relativeL2": rel, "maxAbs": max_abs, "beamformingLossDB": beamforming_loss_db}


def self_test() -> None:
    rng = np.random.default_rng(7)
    h = (rng.normal(size=(4, 8, 32)) + 1j * rng.normal(size=(4, 8, 32))) / np.sqrt(2)
    ref = rzf_precoder(h, 0.1, np.complex128)
    candidate = rzf_precoder(h, 0.1, np.complex64)
    result = metrics(ref, candidate, h)
    if ref.shape != (4, 32, 8) or not np.isfinite(list(result.values())).all():
        raise AssertionError("invalid RZF result")
    if result["relativeL2"] > 1e-4:
        raise AssertionError(result)
    print(json.dumps({"selfTest": "passed", "metrics": result}, indent=2))


def main() -> None:
    p = argparse.ArgumentParser(); p.add_argument("--self-test", action="store_true"); a = p.parse_args()
    if a.self_test: self_test()
    else: p.error("only --self-test is supported; use benchmark.py for measurements")

if __name__ == "__main__": main()
