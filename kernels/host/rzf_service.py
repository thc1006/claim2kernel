#!/usr/bin/env python3
"""stdin-json-v1 RZF service: the Claim2Kernel artifact front-end.

The Claim2Kernel launcher (pkg/launcher) feeds a KernelRequest as JSON on stdin
and sets C2K_CONTRACT_DIGEST / C2K_ARTIFACT_DIGEST / C2K_REQUEST_NAME in the
environment. This service is the reference implementation of that protocol for
the RZF kernel: it reads the request, derives the [batch, ues, antennas] shape,
generates a deterministic seeded channel of that shape, runs the precoder
through the c2k_rzf C ABI, and emits a JSON result with the per-batch status
histogram, an in-process numerical self-check against the complex128 oracle, and
device timing.

It is FAIL-CLOSED: if no backend shared library is present (no Mojo/CUDA build)
or the device rejects the shape, it exits non-zero with a NOT RUN / error status
and never fabricates a result. Strict, size-bounded JSON with duplicate-key
rejection mirrors pkg/jsonsafe and tools/calibration/jsonsafe.py.
"""
from __future__ import annotations

import hashlib
import json
import os
import sys
from pathlib import Path

import numpy as np

_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_ROOT / "kernels" / "rzf"))

from rzf_host import open_backend, C2K_RZF_OK  # noqa: E402
from rzf_reference import metrics, rzf_precoder  # noqa: E402

MAX_STDIN_BYTES = 1 << 20
MAX_BATCH, MAX_USERS, MAX_ANTENNAS = 128, 64, 256
ALPHA = 0.1  # profile-bound regularization weight
# FP32 RZF budget: the in-process check is not merely reported, it gates the
# top-level status. A backend that returns per-batch OK but a numerically wrong
# precoder is a hard failure, not a success.
NUMERICAL_TOLERANCE = 1e-3


def _no_dupes(pairs):
    d = {}
    for k, v in pairs:
        if k in d:
            raise ValueError(f"duplicate JSON key: {k!r}")
        d[k] = v
    return d


def _reject_constant(value):
    raise ValueError(f"non-standard JSON number {value!r} is forbidden")


def fail(msg, code=2):
    json.dump({"protocol": "stdin-json-v1", "status": "error", "error": msg}, sys.stdout)
    sys.stdout.write("\n")
    sys.exit(code)


def _require_int(inputs, key):
    if key not in inputs:
        fail(f"missing required input: {key}")
    v = inputs[key]
    if isinstance(v, bool) or not isinstance(v, (int, float)):
        fail(f"input {key} must be numeric")
    if float(v) != int(v):
        fail(f"input {key} must be integer-valued")
    return int(v)


def main() -> None:
    raw = sys.stdin.buffer.read(MAX_STDIN_BYTES + 1)
    if len(raw) > MAX_STDIN_BYTES:
        fail("request exceeds 1 MiB")
    try:
        req = json.loads(raw.decode("utf-8"), object_pairs_hook=_no_dupes, parse_constant=_reject_constant)
    except Exception as exc:
        fail(f"invalid request JSON: {exc}")

    if req.get("kind") != "KernelRequest":
        fail("not a KernelRequest")
    spec = req.get("spec") or {}
    inputs = spec.get("inputs") or {}
    antennas = _require_int(inputs, "antennas")
    ues = _require_int(inputs, "ues")
    batch = _require_int(inputs, "batch")
    if not (0 < batch <= MAX_BATCH and 0 < ues <= MAX_USERS and 0 < antennas <= MAX_ANTENNAS):
        fail("shape outside supported bounds")
    if ues > antennas:
        fail("U>N unsupported (ues must be <= antennas)")

    backend = open_backend(batch, ues, antennas)
    if backend is None:
        fail("NOT RUN: no c2k_rzf backend shared library present (build Mojo or CUDA first)")

    # Deterministic channel derived from the request identity. hashlib (not the
    # per-process-salted builtin hash()) keeps it reproducible across processes
    # and hosts. This is protocol conformance, not a physical channel model.
    name = req.get("metadata", {}).get("name", "")
    key = f"{name}|{batch}|{ues}|{antennas}".encode("utf-8")
    seed = int.from_bytes(hashlib.sha256(key).digest()[:4], "little")
    rng = np.random.default_rng(seed)
    h = ((rng.normal(size=(batch, ues, antennas))
          + 1j * rng.normal(size=(batch, ues, antennas))) / np.sqrt(2)).astype(np.complex64)

    try:
        with backend:
            w, status, timing = backend.run(h, ALPHA)
            ref = rzf_precoder(h, ALPHA, np.complex128)
            num = metrics(ref, w, h)
            hist = {int(s): int((status == s).sum()) for s in np.unique(status)}
            all_ok = bool((status == C2K_RZF_OK).all())
            within_tol = bool(num["relativeL2"] < NUMERICAL_TOLERANCE)
            if all_ok and within_tol:
                top = "ok"
            elif not all_ok:
                top = "partial"            # some elements rejected -- fail-closed working as intended
            else:
                top = "numerical-mismatch"  # every batch OK yet W diverges from the oracle: backend is wrong
            out = {
                "protocol": "stdin-json-v1",
                "status": top,
                "request": name,
                "backend": backend.backend_name,
                "shape": {"batch": batch, "users": ues, "antennas": antennas},
                "statusHistogram": hist,
                "numerical": {"relativeL2": num["relativeL2"], "maxAbs": num["maxAbs"],
                              "tolerance": NUMERICAL_TOLERANCE, "withinTolerance": within_tol},
                "timingMs": {k: timing[k] for k in ("h2dMs", "kernelMs", "d2hMs", "eventTimed")},
                "contractDigest": os.getenv("C2K_CONTRACT_DIGEST", ""),
                "artifactDigest": os.getenv("C2K_ARTIFACT_DIGEST", ""),
            }
    except Exception as exc:
        fail(f"kernel execution failed: {exc}")

    json.dump(out, sys.stdout, allow_nan=False)
    sys.stdout.write("\n")
    if out["status"] == "numerical-mismatch":
        sys.exit(3)  # fail closed: a numerically wrong precoder is not a success


if __name__ == "__main__":
    main()
