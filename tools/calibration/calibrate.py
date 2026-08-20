#!/usr/bin/env python3
"""Calibrate a Claim2Kernel profile from explicit train/calibration/test CSV splits."""
from __future__ import annotations

import argparse
import csv
import json
import math
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

import numpy as np

from c2k_stats import (
    empirical_coverage,
    fit_mahalanobis,
    fit_ridge,
    sha256_file,
    split_conformal_upper,
    tolerance_upper,
)
from jsonsafe import load_json


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser()
    p.add_argument("--template", required=True)
    p.add_argument("--csv", required=True)
    p.add_argument("--out", required=True)
    p.add_argument("--report", required=True)
    p.add_argument("--method", choices=("split-conformal", "one-sided-tolerance"), default="one-sided-tolerance")
    p.add_argument("--coverage", type=float, default=0.95)
    p.add_argument("--confidence", type=float, default=0.95)
    p.add_argument("--ridge", type=float, default=1e-6)
    p.add_argument("--ood-regularization", type=float, default=1e-4)
    p.add_argument("--features", help="comma-separated model/OOD columns; default derives numeric inputs and interference")
    p.add_argument("--calibrated-at", help="RFC3339 timestamp; default current UTC")
    p.add_argument("--compiler", required=True)
    p.add_argument("--source-digest", required=True)
    p.add_argument("--max-age-seconds", type=int, default=30 * 24 * 3600)
    p.add_argument("--io-budget-us", type=float, default=0.0)
    p.add_argument("--runtime-jitter-us", type=float, default=0.0)
    p.add_argument("--max-rows", type=int, default=1_000_000)
    p.add_argument("--max-csv-bytes", type=int, default=512 << 20)
    p.add_argument(
        "--profile-validity-seconds",
        type=int,
        help="profile validity from calibratedAt; default max-age-seconds",
    )
    return p.parse_args()


def main() -> int:
    args = parse_args()
    profile = load_json(args.template)
    if profile.get("seal") is not None:
        raise ValueError("template must be unsealed")
    if args.max_age_seconds <= 0:
        raise ValueError("max-age-seconds must be > 0")
    if args.profile_validity_seconds is not None and args.profile_validity_seconds <= 0:
        raise ValueError("profile-validity-seconds must be > 0")
    features = parse_features(args.features, profile)
    rows = load_csv(args.csv, features, args.max_rows, args.max_csv_bytes)
    validate_rows_against_envelope(rows, profile, features)
    split = {name: [r for r in rows if r["split"] == name] for name in ("train", "calibration", "test")}
    for name, values in split.items():
        if not values:
            raise ValueError(f"CSV split {name!r} is empty")

    x_train, y_train = matrix(split["train"], features, "latency_us")
    x_cal, y_cal = matrix(split["calibration"], features, "latency_us")
    x_test, y_test = matrix(split["test"], features, "latency_us")
    model = fit_ridge(x_train, y_train, args.ridge)
    cal_residual = y_cal - model.predict(x_cal)
    if args.method == "split-conformal":
        residual_upper, latency_rank = split_conformal_upper(cal_residual, args.coverage)
        achieved_confidence = None
    else:
        residual_upper, latency_rank, achieved_confidence = tolerance_upper(
            cal_residual, args.coverage, args.confidence
        )
    residual_upper = max(0.0, residual_upper)
    test_upper = model.predict(x_test) + residual_upper
    observed_coverage = empirical_coverage(y_test, test_upper)
    if observed_coverage + 1e-12 < args.coverage:
        raise ValueError(
            f"independent latency test coverage {observed_coverage:.6f} is below {args.coverage:.6f}"
        )

    ood_model = fit_mahalanobis(x_train, args.ood_regularization)
    cal_scores = ood_model.score(x_cal)
    ood_threshold, ood_rank = split_conformal_upper(cal_scores, args.coverage)
    test_scores = ood_model.score(x_test)
    test_inlier_rate = float(np.mean(test_scores <= ood_threshold))
    if test_inlier_rate + 1e-12 < args.coverage:
        raise ValueError(
            f"independent OOD inlier rate {test_inlier_rate:.6f} is below {args.coverage:.6f}"
        )

    cal_error = np.asarray([float(r["numerical_error"]) for r in split["calibration"]])
    test_error = np.asarray([float(r["numerical_error"]) for r in split["test"]])
    if (
        not np.isfinite(cal_error).all()
        or not np.isfinite(test_error).all()
        or np.min(cal_error) < 0
        or np.min(test_error) < 0
    ):
        raise ValueError("numerical_error must be finite and non-negative")
    # A conservative v1alpha1 rule: the calibration maximum is the certificate;
    # the independent test set must not exceed it. This avoids choosing the bound
    # after seeing test outcomes.
    numerical_upper = float(np.max(cal_error))
    test_error_max = float(np.max(test_error))
    if test_error_max > numerical_upper + 1e-15:
        raise ValueError(
            f"independent numerical test max {test_error_max:.6g} exceeds calibration bound {numerical_upper:.6g}"
        )

    calibrated_dt = parse_rfc3339(args.calibrated_at) if args.calibrated_at else datetime.now(timezone.utc)
    calibrated_at = format_rfc3339(calibrated_dt)
    validity_seconds = args.profile_validity_seconds or args.max_age_seconds
    profile["metadata"]["createdAt"] = calibrated_at
    profile["metadata"]["expiresAt"] = format_rfc3339(calibrated_dt + timedelta(seconds=validity_seconds))
    coefficients = {name: float(value) for name, value in zip(features, model.coefficients, strict=True)}
    latency = profile["spec"]["latency"]
    latency.update(
        {
            "method": args.method,
            "quantile": args.coverage,
            "confidence": args.confidence,
            "residualUpperUS": residual_upper,
            "ioBudgetUS": args.io_budget_us,
            "runtimeJitterUS": args.runtime_jitter_us,
            "calibrationSampleCount": len(split["calibration"]),
            "testSampleCount": len(split["test"]),
            "observedCoverage": observed_coverage,
            "model": {
                "interceptUS": model.intercept,
                "coefficients": coefficients,
                "featureOrder": features,
                "ridgeLambda": args.ridge,
            },
            "calibratedAt": calibrated_at,
            "maxAgeSeconds": args.max_age_seconds,
        }
    )
    profile["spec"]["ood"] = {
        "method": "mahalanobis-conformal",
        "required": True,
        "features": features,
        "mean": ood_model.mean.tolist(),
        "inverseCovariance": ood_model.inverse_covariance.tolist(),
        "threshold": float(ood_threshold),
        "coverage": args.coverage,
        "calibrationSampleCount": len(split["calibration"]),
        "observedTestInlierRate": test_inlier_rate,
        "regularization": args.ood_regularization,
    }
    profile["spec"]["numerical"].update(
        {
            "upperBound": numerical_upper,
            "observedMax": test_error_max,
            "testSampleCount": len(split["test"]),
        }
    )
    profile["spec"]["provenance"].update(
        {
            "compiler": args.compiler,
            "sourceDigest": args.source_digest,
            "datasetDigest": sha256_file(args.csv),
        }
    )
    profile.pop("seal", None)
    report: dict[str, Any] = {
        "schemaVersion": 1,
        "datasetDigest": sha256_file(args.csv),
        "features": features,
        "splitCounts": {k: len(v) for k, v in split.items()},
        "latency": {
            "method": args.method,
            "coverageTarget": args.coverage,
            "confidenceTarget": args.confidence,
            "orderStatisticRank": latency_rank,
            "achievedToleranceConfidence": achieved_confidence,
            "residualUpperUS": residual_upper,
            "independentObservedCoverage": observed_coverage,
            "testMaxExcessUS": float(np.max(y_test - test_upper)),
        },
        "ood": {
            "method": "regularized Mahalanobis + split-conformal threshold",
            "thresholdRank": ood_rank,
            "threshold": float(ood_threshold),
            "conditionNumber": ood_model.condition_number,
            "independentInlierRate": test_inlier_rate,
            "warning": "This is a calibrated rejection heuristic, not a universal OOD detector.",
        },
        "numerical": {
            "calibrationMaximum": numerical_upper,
            "independentTestMaximum": test_error_max,
        },
        "dataHygiene": {
            "sampleIDsUniqueAcrossAllSplits": True,
            "groupsDisjointAcrossSplits": True,
            "splitAssignmentSource": "input CSV; must be frozen before calibration",
        },
        "claims": {
            "hardRealtime": False,
            "coverageScope": "marginal under exchangeability and the declared envelope",
            "testUsedForFitting": False,
        },
    }
    write_json(args.out, profile)
    write_json(args.report, report)
    return 0


def parse_features(raw: str | None, profile: dict[str, Any]) -> list[str]:
    if raw:
        result = [x.strip() for x in raw.split(",") if x.strip()]
    else:
        result = []
        for name, spec in sorted(profile["spec"]["inputDomain"]["features"].items()):
            if spec["kind"] in ("number", "integer") and spec.get("oodFeature", False):
                result.append("input." + name)
        result.extend("interference." + n for n in sorted(profile["spec"]["interference"]["metrics"]))
    if not result or len(result) != len(set(result)):
        raise ValueError("features must be a non-empty unique list")
    return result


def load_csv(
    path: str, features: list[str], max_rows: int, max_bytes: int
) -> list[dict[str, str]]:
    source = Path(path)
    if max_rows <= 0 or max_rows > 10_000_000:
        raise ValueError("max-rows must be in [1,10000000]")
    if max_bytes <= 0 or max_bytes > 4 << 30:
        raise ValueError("max-csv-bytes must be in [1,4GiB]")
    size = source.stat().st_size
    if size == 0 or size > max_bytes:
        raise ValueError(f"CSV size {size} is outside (0,{max_bytes}]")
    with source.open(newline="") as f:
        reader = csv.DictReader(f)
        required = {
            "sample_id",
            "group_id",
            "split",
            "latency_us",
            "numerical_error",
            *features,
        }
        if reader.fieldnames is None or set(reader.fieldnames) != required:
            raise ValueError(
                f"CSV columns must exactly equal {sorted(required)}, got {reader.fieldnames}"
            )
        rows: list[dict[str, str]] = []
        seen_samples: set[str] = set()
        group_splits: dict[str, str] = {}
        for i, row in enumerate(reader, start=2):
            if len(rows) >= max_rows:
                raise ValueError(f"CSV exceeds max-rows={max_rows}")
            sample_id = row["sample_id"].strip()
            group_id = row["group_id"].strip()
            if not sample_id or len(sample_id) > 256 or "\x00" in sample_id:
                raise ValueError(f"row {i}: invalid sample_id")
            if sample_id in seen_samples:
                raise ValueError(f"row {i}: duplicate sample_id {sample_id!r}")
            seen_samples.add(sample_id)
            if not group_id or len(group_id) > 256 or "\x00" in group_id:
                raise ValueError(f"row {i}: invalid group_id")
            split = row["split"]
            if split not in {"train", "calibration", "test"}:
                raise ValueError(f"row {i}: invalid split {split!r}")
            previous = group_splits.setdefault(group_id, split)
            if previous != split:
                raise ValueError(
                    f"row {i}: group_id {group_id!r} crosses splits {previous!r} and {split!r}"
                )
            for col in features + ["latency_us", "numerical_error"]:
                value = float(row[col])
                if not math.isfinite(value):
                    raise ValueError(f"row {i}: {col} is non-finite")
            if float(row["latency_us"]) < 0 or float(row["numerical_error"]) < 0:
                raise ValueError(f"row {i}: latency_us and numerical_error must be non-negative")
            rows.append(row)
    if not rows:
        raise ValueError("CSV contains no data rows")
    return rows


def validate_rows_against_envelope(rows: list[dict[str, str]], profile: dict[str, Any], features: list[str]) -> None:
    input_specs = profile["spec"]["inputDomain"]["features"]
    interference = profile["spec"]["interference"]["metrics"]
    for index, row in enumerate(rows, start=2):
        for feature in features:
            value = float(row[feature])
            if feature.startswith("input."):
                name = feature.removeprefix("input.")
                spec = input_specs.get(name)
                if not spec or spec["kind"] not in ("number", "integer"):
                    raise ValueError(f"feature {feature} is not a declared numeric input")
                if not (float(spec["minimum"]) <= value <= float(spec["maximum"])):
                    raise ValueError(f"row {index}: {feature} is outside the declared envelope")
                if spec["kind"] == "integer" and not value.is_integer():
                    raise ValueError(f"row {index}: {feature} must be integer-valued")
            elif feature.startswith("interference."):
                name = feature.removeprefix("interference.")
                spec = interference.get(name)
                if not spec or not (float(spec["minimum"]) <= value <= float(spec["maximum"])):
                    raise ValueError(f"row {index}: {feature} is outside the declared envelope")
            else:
                raise ValueError(f"unsupported feature namespace: {feature}")


def matrix(rows: list[dict[str, str]], features: list[str], target: str) -> tuple[np.ndarray, np.ndarray]:
    x = np.asarray([[float(r[f]) for f in features] for r in rows], dtype=np.float64)
    y = np.asarray([float(r[target]) for r in rows], dtype=np.float64)
    return x, y


def parse_rfc3339(value: str) -> datetime:
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise ValueError("calibrated-at must be RFC3339") from exc
    if parsed.tzinfo is None:
        raise ValueError("calibrated-at must include a timezone")
    return parsed.astimezone(timezone.utc)


def format_rfc3339(value: datetime) -> str:
    return value.astimezone(timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def write_json(path: str, value: Any) -> None:
    out = Path(path)
    out.parent.mkdir(parents=True, exist_ok=True)
    temp = out.with_name("." + out.name + ".tmp")
    temp.write_text(json.dumps(value, indent=2, sort_keys=False, allow_nan=False) + "\n")
    temp.replace(out)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # explicit single-line CLI failure, no partial output
        print(f"calibrate: {exc}", file=sys.stderr)
        raise SystemExit(1)
