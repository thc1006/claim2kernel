#!/usr/bin/env python3
"""Generate deterministic CI-only RZF-like calibration data.

The rows are synthetic and exist solely to exercise the pipeline. They are not
hardware evidence and MUST NOT be used in a paper's performance tables.
"""
from __future__ import annotations

import argparse
import csv
from pathlib import Path

import numpy as np


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True)
    parser.add_argument("--seed", type=int, default=20260820)
    args = parser.parse_args()
    rng = np.random.default_rng(args.seed)
    rows: list[dict[str, float | int | str]] = []

    sample_index = 0
    for split, count in (("train", 400), ("calibration", 400), ("test", 200)):
        for local_index in range(count):
            antennas = int(rng.choice([16, 32, 64, 128]))
            max_ues = min(32, antennas)
            # Keep the independent test split narrower; this makes the smoke
            # pipeline deterministic without pretending to validate OOD.
            if split == "test":
                antennas = int(rng.choice([32, 64]))
                max_ues = min(16, antennas)
            ues = int(rng.integers(1, max_ues + 1))
            batch = int(rng.integers(1, 33 if split != "test" else 17))
            gpu_busy = float(rng.uniform(0.0, 0.7 if split != "test" else 0.45))
            # Deliberately nonlinear synthetic surface; ridge + residual must
            # absorb misspecification instead of receiving a perfect toy line.
            base = 25.0 + 0.03 * antennas * ues + 0.8 * batch + 130.0 * gpu_busy
            noise_scale = 8.0 if split == "calibration" else 3.0
            latency = base + abs(float(rng.normal(0.0, noise_scale)))
            numerical_error = float(rng.uniform(0.0, 8e-4 if split == "calibration" else 4e-4))
            rows.append(
                {
                    "sample_id": f"{split}-{sample_index:06d}",
                    "group_id": f"{split}-run-{local_index // 20:04d}",
                    "split": split,
                    "input.antennas": antennas,
                    "input.ues": ues,
                    "input.batch": batch,
                    "interference.gpu_busy": gpu_busy,
                    "latency_us": latency,
                    "numerical_error": numerical_error,
                }
            )
            sample_index += 1

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    with out.open("w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    print(f"wrote {len(rows)} synthetic CI-only rows to {out}")


if __name__ == "__main__":
    main()
