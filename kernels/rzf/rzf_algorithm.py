#!/usr/bin/env python3
"""Explicit-Cholesky RZF precoder: the executable spec mirrored by the GPU kernels.

`kernels/rzf/rzf_reference.py` is the *authoritative* complex128 oracle. It uses
`numpy.linalg.solve`, which is convenient but hides the exact factorization the
GPU kernel must perform. This module implements the **same mathematical map**
using an explicit Hermitian Cholesky factorization plus forward/back
substitution -- step for step what `kernels/mojo/rzf_kernel.mojo` and
`kernels/cuda/rzf_cuda.cu` do on device. It exists so that:

  * the GPU algorithm design is validated on CPU against the oracle before any
    hardware is touched (no explicit matrix inverse is ever formed);
  * the *fail-closed* per-batch status semantics (non-finite input, non
    positive-definite regularized Gram) are specified in one place and shared by
    the correctness tests and the ctypes host;
  * complex64 (FP32) storage/accumulation is exercised exactly as the kernel
    will, so the numerical budget in the profile is grounded in a real run.

Mathematical definition (identical to the oracle):

    G   = H H^H                       # [B,U,U] Hermitian PSD
    A   = G + alpha I                  # [B,U,U] Hermitian positive-definite
    A X = H                            # solve, X = [B,U,N]  (no inverse)
    W   = conj(X)^T                    # [B,N,U]
    W   = W / max(||W||_F, tiny)       # per-batch Frobenius normalization

Because W = conj(X)^T is a conjugate transpose, ||W||_F == ||X||_F.

Status codes are shared with the C ABI (`kernels/include/c2k_rzf.h`).
"""
from __future__ import annotations

import numpy as np

STATUS_OK = 0
STATUS_NON_FINITE_INPUT = 1
STATUS_NOT_POSITIVE_DEFINITE = 2
STATUS_INVALID_SHAPE = 3

STATUS_NAMES = {
    STATUS_OK: "OK",
    STATUS_NON_FINITE_INPUT: "NON_FINITE_INPUT",
    STATUS_NOT_POSITIVE_DEFINITE: "NOT_POSITIVE_DEFINITE",
    STATUS_INVALID_SHAPE: "INVALID_SHAPE",
}


def real_dtype(dtype: np.dtype) -> np.dtype:
    """Return the real component dtype (complex64 -> float32, complex128 -> float64)."""
    return np.empty((), dtype=dtype).real.dtype


def _tiny(dtype: np.dtype) -> float:
    return float(np.finfo(real_dtype(dtype)).tiny)


def validate_shape(batch: int, users: int, antennas: int, alpha: float) -> None:
    """Hard shape/parameter gate. Mirrors the oracle's ValueError and the ABI's
    STATUS_INVALID_SHAPE. U > N (antennas < users) is rejected here."""
    if batch <= 0 or users <= 0 or antennas <= 0:
        raise ValueError("require batch/users/antennas > 0")
    if antennas < users:
        raise ValueError(f"U>N unsupported: users={users} > antennas={antennas}")
    if not (alpha > 0):
        raise ValueError("regularization alpha must be > 0")


def rzf_precoder_cholesky(
    channel: np.ndarray,
    alpha: float,
    dtype=np.complex64,
    *,
    deterministic: bool = True,
) -> tuple[np.ndarray, np.ndarray]:
    """RZF precoder via explicit Hermitian Cholesky. Fail-closed per batch.

    Returns ``(W, status)`` where ``W`` has shape ``[B, N, U]`` in ``dtype`` and
    ``status`` has shape ``[B]`` (int32). On any non-OK status the corresponding
    ``W[b]`` is all zeros -- a rejected element never leaks NaN/Inf or an
    unnormalized precoder downstream.

    ``deterministic`` selects a fixed left-to-right accumulation order for the
    Gram and the triangular solves, matching the GPU deterministic mode. It is
    accepted for parity; this CPU mirror is deterministic in both settings.
    """
    h_in = np.asarray(channel)
    if h_in.ndim != 3:
        raise ValueError("channel must have shape [batch, users, antennas]")
    batch, users, antennas = (int(h_in.shape[0]), int(h_in.shape[1]), int(h_in.shape[2]))
    validate_shape(batch, users, antennas, alpha)

    cdt = np.dtype(dtype)
    rdt = real_dtype(cdt)
    a = rdt.type(alpha)
    tiny = rdt.type(_tiny(cdt))

    h = h_in.astype(cdt, copy=True)
    out = np.zeros((batch, antennas, users), dtype=cdt)
    status = np.zeros((batch,), dtype=np.int32)

    # Overflow/invalid are expected and handled as fail-closed status below (the
    # GPU kernel does the same via isfinite pivots); do not warn on them.
    with np.errstate(over="ignore", invalid="ignore", divide="ignore"):
      for b in range(batch):
        hb = h[b]  # [U, N]
        if not (np.isfinite(hb.real).all() and np.isfinite(hb.imag).all()):
            status[b] = STATUS_NON_FINITE_INPUT
            continue

        # Regularized Hermitian Gram A = H H^H + alpha I, accumulated in `dtype`.
        gram = _gram(hb, cdt, deterministic)
        for i in range(users):
            gram[i, i] = (gram[i, i].real + a).astype(rdt).astype(cdt)

        lower, positive_definite = _cholesky(gram, cdt, rdt, deterministic)
        if not positive_definite:
            status[b] = STATUS_NOT_POSITIVE_DEFINITE
            continue

        # Solve A X = H column by column (each column is an independent solve).
        x = np.zeros((users, antennas), dtype=cdt)
        for n in range(antennas):
            x[:, n] = _solve_spd(lower, hb[:, n], cdt, rdt, deterministic)

        w = np.conj(x).T.astype(cdt)  # [N, U]; ||w||_F == ||x||_F
        sq = (w.real.astype(rdt) ** 2 + w.imag.astype(rdt) ** 2).sum(dtype=rdt)
        norm = rdt.type(np.sqrt(sq))
        scale = rdt.type(1.0) / max(norm, tiny)
        out[b] = (w * scale).astype(cdt)

    return out, status


def _gram(hb: np.ndarray, cdt: np.dtype, deterministic: bool) -> np.ndarray:
    """A = H H^H, entry (i,j) = sum_n H[i,n] conj(H[j,n]), accumulated in `cdt`."""
    users = hb.shape[0]
    gram = np.zeros((users, users), dtype=cdt)
    for i in range(users):
        row = hb[i, :]
        for j in range(users):
            prod = (row * np.conj(hb[j, :])).astype(cdt)
            gram[i, j] = prod.sum(dtype=cdt)
    return gram


def _cholesky(a: np.ndarray, cdt: np.dtype, rdt: np.dtype, deterministic: bool):
    """Lower Cholesky of a Hermitian matrix; second return is positive-definite flag.

    Non-finite or non-positive pivots (an ill-conditioned regularized Gram) stop
    the factorization and report ``positive_definite=False`` -- the fail-closed
    signal the caller turns into STATUS_NOT_POSITIVE_DEFINITE."""
    n = a.shape[0]
    lower = np.zeros((n, n), dtype=cdt)
    for j in range(n):
        acc = a[j, j].real
        for k in range(j):
            acc = rdt.type(acc - (lower[j, k] * np.conj(lower[j, k])).real)
        if not np.isfinite(acc) or acc <= 0:
            return lower, False
        ljj = rdt.type(np.sqrt(acc))
        lower[j, j] = cdt.type(ljj)
        for i in range(j + 1, n):
            v = a[i, j]
            for k in range(j):
                v = (v - lower[i, k] * np.conj(lower[j, k])).astype(cdt)
            lower[i, j] = (v / ljj).astype(cdt)
    return lower, True


def _solve_spd(lower: np.ndarray, rhs: np.ndarray, cdt: np.dtype, rdt: np.dtype,
               deterministic: bool) -> np.ndarray:
    """Solve (L L^H) x = rhs via forward then backward substitution."""
    n = lower.shape[0]
    y = np.zeros(n, dtype=cdt)
    for i in range(n):
        acc = rhs[i]
        for k in range(i):
            acc = (acc - lower[i, k] * y[k]).astype(cdt)
        y[i] = (acc / lower[i, i].real).astype(cdt)
    x = np.zeros(n, dtype=cdt)
    for i in range(n - 1, -1, -1):
        acc = y[i]
        for k in range(i + 1, n):
            acc = (acc - np.conj(lower[k, i]) * x[k]).astype(cdt)
        x[i] = (acc / lower[i, i].real).astype(cdt)
    return x


if __name__ == "__main__":
    # Minimal self-check against the authoritative oracle.
    import json
    from rzf_reference import metrics, rzf_precoder

    rng = np.random.default_rng(7)
    h = (rng.normal(size=(4, 8, 32)) + 1j * rng.normal(size=(4, 8, 32))) / np.sqrt(2)
    ref = rzf_precoder(h, 0.1, np.complex128)
    w128, st128 = rzf_precoder_cholesky(h, 0.1, np.complex128)
    w64, st64 = rzf_precoder_cholesky(h, 0.1, np.complex64)
    m128 = metrics(ref, w128, h)
    m64 = metrics(ref, w64, h)
    ok = (
        st128.sum() == 0
        and st64.sum() == 0
        and m128["relativeL2"] < 1e-10
        and m64["relativeL2"] < 1e-4
    )
    print(json.dumps({"selfCheck": "passed" if ok else "FAILED",
                      "complex128VsOracle": m128, "complex64VsOracle": m64}, indent=2))
    if not ok:
        raise SystemExit(1)
