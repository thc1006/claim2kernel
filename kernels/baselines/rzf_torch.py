#!/usr/bin/env python3
"""PyTorch/cuSOLVER RZF GPU baseline (the tuned vendor-library reference).

`torch.linalg.solve` dispatches to cuSOLVER on CUDA and to rocSOLVER on ROCm, so
this single file is the vendor-library baseline for BOTH the NVIDIA and AMD
cross-stack comparisons required by docs/MOJO_HARDWARE_PROTOCOL.md phase 8. It
performs a Hermitian solve (no explicit inverse), matching the kernels' math and
FP32 precision.

STATUS in this environment: NOT RUN -- PyTorch is not installed and the only
local GPU is a GTX 1050 (sm_61), below the arch that recent PyTorch CUDA wheels
target. The source is complete; run it on a supported host to populate the
cuSOLVER/rocSOLVER column of the comparison.
"""
from __future__ import annotations


def rzf_torch(channel, alpha: float):
    """channel: complex64 torch.Tensor [B,U,N] on a CUDA/ROCm device. Returns W [B,N,U]."""
    import torch

    h = channel.to(torch.complex64)
    if h.dim() != 3:
        raise ValueError("channel must be [batch, users, antennas]")
    b, u, n = h.shape
    if n < u or not (alpha > 0):
        raise ValueError("require antennas>=users and alpha>0")
    eye = torch.eye(u, dtype=torch.complex64, device=h.device).expand(b, u, u)
    gram = h @ h.conj().transpose(-1, -2)                 # [B,U,U] = H H^H
    reg = gram + alpha * eye                              # Hermitian PD
    solved = torch.linalg.solve(reg, h)                   # A X = H (cuSOLVER/rocSOLVER)
    w = solved.conj().transpose(-1, -2)                   # [B,N,U]
    norm = torch.linalg.vector_norm(w.reshape(b, -1), dim=1).reshape(b, 1, 1)
    tiny = torch.finfo(torch.float32).tiny
    return w / torch.clamp(norm, min=tiny)


if __name__ == "__main__":
    import json
    try:
        import torch
    except Exception as exc:
        print(json.dumps({"status": f"NOT RUN: PyTorch unavailable ({type(exc).__name__})"}))
        raise SystemExit(0)
    if not torch.cuda.is_available():
        print(json.dumps({"status": "NOT RUN: no CUDA/ROCm device visible to torch"}))
        raise SystemExit(0)
    import sys
    from pathlib import Path
    sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "rzf"))
    import numpy as np
    from rzf_reference import metrics, rzf_precoder  # type: ignore

    rng = np.random.default_rng(7)
    h = (rng.normal(size=(4, 8, 32)) + 1j * rng.normal(size=(4, 8, 32))) / np.sqrt(2)
    ref = rzf_precoder(h, 0.1, np.complex128)
    w = rzf_torch(torch.from_numpy(h.astype(np.complex64)).cuda(), 0.1).cpu().numpy()
    print(json.dumps({"torchBaselineVsOracle": metrics(ref, w, h)}, indent=2))
