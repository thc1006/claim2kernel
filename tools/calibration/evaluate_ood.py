#!/usr/bin/env python3
"""Explain the calibrated Mahalanobis OOD decision for one request."""
from __future__ import annotations

import argparse
import json
import math

import numpy as np

from jsonsafe import load_json


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", required=True)
    parser.add_argument("--request", required=True)
    args = parser.parse_args()
    profile = load_json(args.profile)
    request = load_json(args.request)
    ood = profile["spec"]["ood"]
    values: list[float] = []
    for name in ood["features"]:
        if name.startswith("input."):
            value = request["spec"]["inputs"][name.removeprefix("input.")]
        elif name.startswith("interference."):
            value = request["spec"]["interference"][name.removeprefix("interference.")]
        else:
            raise ValueError(f"unsupported feature {name}")
        values.append(float(value))
    delta = np.asarray(values, dtype=np.float64) - np.asarray(ood["mean"], dtype=np.float64)
    matrix = np.asarray(ood["inverseCovariance"], dtype=np.float64)
    score = float(delta @ matrix @ delta)
    result = {
        "features": dict(zip(ood["features"], values, strict=True)),
        "score": score,
        "threshold": ood["threshold"],
        "inDistribution": math.isfinite(score) and score >= 0 and score <= ood["threshold"],
        "warning": "A passing Mahalanobis score is necessary, not proof that the request is in-distribution.",
    }
    print(json.dumps(result, indent=2, allow_nan=False))


if __name__ == "__main__":
    main()
