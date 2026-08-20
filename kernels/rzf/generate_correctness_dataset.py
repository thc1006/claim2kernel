#!/usr/bin/env python3
"""Generate the deterministic RZF correctness dataset that is digest-bound.

The dataset is the frozen set of channel shapes/seeds plus the complex128 oracle
outcome for each. Its SHA-256 is written into the Claim2Kernel profile as
`provenance.datasetDigest`, so the correctness evidence a profile refers to is
pinned. Rows are fully deterministic (fixed seeds, sorted keys, no timestamps,
no NaN) so the digest is reproducible on any host.

This is NOT hardware performance data. It records reference outputs and the
FP32-vs-complex128 agreement only.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

import numpy as np

from rzf_algorithm import (
    STATUS_NON_FINITE_INPUT,
    STATUS_NOT_POSITIVE_DEFINITE,
    STATUS_OK,
    rzf_precoder_cholesky,
)
from rzf_reference import metrics, rzf_precoder

# (name, batch, users, antennas, seed, alpha, expected worst-case status)
CASES = [
    ("well-conditioned-4x8x32", 4, 8, 32, 7, 0.1, STATUS_OK),
    ("square-1x8x8", 1, 8, 8, 21, 0.1, STATUS_OK),
    ("single-user-2x1x4", 2, 1, 4, 22, 0.1, STATUS_OK),
    ("odd-5x7x13", 5, 7, 13, 32, 0.1, STATUS_OK),
    ("odd-3x3x5", 3, 3, 5, 31, 0.1, STATUS_OK),
    ("wide-1x2x64", 1, 2, 64, 24, 0.1, STATUS_OK),
    ("moderate-range-2x4x16", 2, 4, 16, 5, 0.1, STATUS_OK),
]


def _channel(batch, users, antennas, seed):
    rng = np.random.default_rng(seed)
    return ((rng.normal(size=(batch, users, antennas))
             + 1j * rng.normal(size=(batch, users, antennas))) / np.sqrt(2))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    rows = []
    for name, b, u, n, seed, alpha, want in CASES:
        h = _channel(b, u, n, seed)
        ref = rzf_precoder(h, alpha, np.complex128)
        w64, status = rzf_precoder_cholesky(h, alpha, np.complex64)
        # Hash the canonical complex128 reference bytes (C-order, little-endian).
        ref_bytes = np.ascontiguousarray(ref, dtype="<c16").tobytes()
        rows.append({
            "name": name, "batch": b, "users": u, "antennas": n,
            "seed": seed, "alpha": alpha,
            "expectedWorstStatus": int(want),
            "statusObserved": [int(x) for x in status],
            "referenceSHA256": hashlib.sha256(ref_bytes).hexdigest(),
            "relativeL2Complex64": float(metrics(ref, w64, h)["relativeL2"]),
        })

    dataset = {
        "schema": "claim2kernel.dev/rzf-correctness-dataset-v1",
        "note": "Deterministic RZF correctness reference. Not performance data.",
        "oracle": "complex128 (kernels/rzf/rzf_reference.py)",
        "cases": rows,
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    # Sorted keys + fixed separators => reproducible bytes => reproducible digest.
    payload = json.dumps(dataset, indent=2, sort_keys=True, allow_nan=False) + "\n"
    out.write_text(payload)
    digest = hashlib.sha256(payload.encode("utf-8")).hexdigest()
    print(json.dumps({"written": str(out), "cases": len(rows), "datasetDigest": "sha256:" + digest}))


if __name__ == "__main__":
    main()
