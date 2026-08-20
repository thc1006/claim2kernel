#!/usr/bin/env python3
"""Cross-stack RZF comparison: NumPy vs CUDA/Mojo GPU vs Triton vs HIP.

Implements the "cross-stack baselines" requirement of the Mojo hardware protocol
under matched rules: identical input, identical FP32 precision target, identical
warm-up policy, and the SAME complex128 oracle as the correctness reference for
every backend. Each backend reports numerical agreement (relative L2 vs oracle)
and a latency summary. A backend whose toolchain/library is absent is recorded
as NOT RUN -- never estimated, never copied from another backend.

Runnable now: NumPy (CPU) and whichever c2k_rzf shared library is present (the
CUDA baseline on this host's GTX 1050). Triton and HIP are NOT RUN here (no
Triton install; no AMD device).
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import numpy as np

_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_ROOT / "kernels" / "rzf"))
sys.path.insert(0, str(_ROOT / "kernels" / "baselines"))

from bench_rzf import rand_channel, summarize, now_ns  # noqa: E402
from rzf_host import open_backend, C2K_RZF_OK  # noqa: E402
from rzf_numpy import rzf_numpy  # noqa: E402
from rzf_reference import metrics, rzf_precoder  # noqa: E402


def _latency(fn, reps, warmup):
    for _ in range(warmup):
        fn()
    s = []
    for _ in range(reps):
        t0 = now_ns()
        fn()
        s.append((now_ns() - t0) / 1e3)  # us
    return summarize(s, "us")


def compare_shape(shape, alpha, reps, warmup, lib):
    b, u, n = shape
    h = rand_channel(b, u, n, seed=7000 + b * 100 + u * 10 + n)
    ref = rzf_precoder(h, alpha, np.complex128)
    backends = {}

    # NumPy CPU baseline (always runnable).
    cand = rzf_numpy(h, alpha, np.complex64)
    backends["numpy-cpu-complex64"] = {
        "present": True, "evidence": "measured-cpu-reference", "notGPU": True,
        "relativeL2VsOracle": metrics(ref, cand, h)["relativeL2"],
        "latency": _latency(lambda: rzf_numpy(h, alpha, np.complex64), reps, warmup),
    }

    # c2k_rzf GPU backend (CUDA baseline here; the Mojo target when built).
    be = open_backend(b, u, n, lib=lib)
    if be is None:
        backends["c2k-rzf-gpu"] = {"present": False, "status": "NOT RUN: no c2k_rzf shared library built"}
    else:
        with be:
            wg, sg, _ = be.run(h, alpha)
            device = "unknown"
            backends[f"c2k-rzf-{be.backend_name}"] = {
                "present": True, "evidence": "measured-gpu",
                "backend": be.backend_name, "library": be.lib_path.name,
                "statusAllOK": bool((sg == C2K_RZF_OK).all()),
                "relativeL2VsOracle": metrics(ref, wg, h)["relativeL2"],
                "endToEndLatency": _latency(lambda: be.run(h, alpha), reps, warmup),
                "note": "device recorded in the benchmark JSON; on this host a GTX "
                        "1050 (sm_61) -- NOT a target (sm_80/sm_90/gfx942) result",
            }

    # Vendor library (cuSOLVER/rocSOLVER via torch) and HIP: source provided,
    # not runnable in this environment.
    backends["torch-cusolver-gpu"] = _try_torch(h, ref, alpha, reps, warmup)
    backends["hip-gpu"] = {"present": False,
                           "status": "NOT RUN: no AMD gfx942 device; hipify kernels/cuda/rzf_cuda.cu to build"}
    return {"shape": shape, "alpha": alpha, "backends": backends}


def _try_torch(h, ref, alpha, reps, warmup):
    try:
        import torch
    except Exception as exc:
        return {"present": False, "status": f"NOT RUN: PyTorch unavailable ({type(exc).__name__})"}
    try:
        from rzf_torch import rzf_torch  # type: ignore
        if not torch.cuda.is_available():
            return {"present": False, "status": "NOT RUN: torch.cuda not available"}
        ht = torch.from_numpy(h.astype(np.complex64)).cuda()
        w = rzf_torch(ht, alpha).cpu().numpy()
        return {"present": True, "evidence": "measured-gpu", "backend": "torch-cusolver",
                "relativeL2VsOracle": metrics(ref, w, h)["relativeL2"],
                "endToEndLatency": _latency(lambda: rzf_torch(ht, alpha), reps, warmup)}
    except Exception as exc:  # pragma: no cover
        return {"present": False, "status": f"NOT RUN: torch baseline error ({exc})"}


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--out", required=True)
    p.add_argument("--reps", type=int, default=100)
    p.add_argument("--warmup", type=int, default=20)
    p.add_argument("--alpha", type=float, default=0.1)
    p.add_argument("--backend-lib", default=None)
    p.add_argument("--shapes", default="1x4x32;4x8x64;4x16x128")
    args = p.parse_args()

    shapes = [tuple(int(x) for x in s.lower().split("x")) for s in args.shapes.split(";")]
    result = {
        "schema": "claim2kernel.dev/rzf-comparison-v1",
        "rules": {"precision": "complex64", "oracle": "complex128 (kernels/rzf/rzf_reference.py)",
                  "matchedInput": True, "matchedWarmup": args.warmup},
        "cases": [compare_shape(list(s), args.alpha, args.reps, args.warmup, args.backend_lib) for s in shapes],
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(result, indent=2, allow_nan=False) + "\n")

    # Compact human-readable table to stdout.
    print(f"{'shape':12s} {'backend':22s} {'present':8s} {'relL2':12s} {'e2e_p50_us':10s}")
    for case in result["cases"]:
        shp = "x".join(str(x) for x in case["shape"])
        for name, info in case["backends"].items():
            present = "yes" if info.get("present") else "NOT RUN"
            rel = f"{info['relativeL2VsOracle']:.3e}" if "relativeL2VsOracle" in info else "-"
            lat = info.get("endToEndLatency") or info.get("latency") or {}
            p50 = f"{lat.get('p50', 0):.2f}" if lat.get("p50") is not None else "-"
            print(f"{shp:12s} {name:22s} {present:8s} {rel:12s} {p50:10s}")
    print(json.dumps({"written": str(out)}))


if __name__ == "__main__":
    main()
