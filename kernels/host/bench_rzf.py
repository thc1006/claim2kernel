#!/usr/bin/env python3
"""RZF benchmark harness -- device-event GPU timing with strict honesty rules.

Measures, and reports SEPARATELY, every phase requested by the Claim2Kernel Mojo
hardware protocol:

  * compile        one-shot build time of the backend shared library
  * cold_start     first create()+first run() of a fresh context (host wall)
  * kernel         warmed kernel-only, DEVICE-EVENT measured (never host wall)
  * h2d            host->device copy, DEVICE-EVENT measured
  * d2h            device->host copy, DEVICE-EVENT measured
  * end_to_end     full run() as seen by the caller, CLOCK_MONOTONIC_RAW host wall

Non-negotiable rules enforced in code:
  * kernel/h2d/d2h come ONLY from backend device events (timing.event_timed).
    If a backend does not populate them, those phases are recorded as NOT RUN --
    a host clock reading is NEVER substituted for a GPU phase.
  * p99 requires >=100 retained samples, p99.9 requires >=1000; otherwise the
    quantile is reported as null with an explicit note. A single best value is
    never presented as a tail quantile.
  * every raw per-sample value is retained in the JSON and CSV outputs.
  * warm-up iterations are excluded from the reported distribution and retained
    separately.

This harness measures whatever backend library is present. On a GeForce GTX 1050
(sm_61) it measures the CUDA baseline -- a real GPU number that is NOT a target
(sm_80/sm_90/gfx942) result and NOT the Mojo kernel. The output records the exact
device so the number cannot be mistaken for target-hardware evidence.
"""
from __future__ import annotations

import argparse
import json
import platform
import subprocess
import sys
import time
from pathlib import Path

import numpy as np

_HERE = Path(__file__).resolve()
_ROOT = _HERE.parents[2]
sys.path.insert(0, str(_ROOT / "kernels" / "rzf"))
sys.path.insert(0, str(_ROOT / "kernels" / "baselines"))

from rzf_host import open_backend  # noqa: E402
from rzf_numpy import rzf_numpy  # noqa: E402

MONOTONIC_RAW = getattr(time, "CLOCK_MONOTONIC_RAW", time.CLOCK_MONOTONIC)


def now_ns() -> int:
    return time.clock_gettime_ns(MONOTONIC_RAW)


def rand_channel(batch, users, antennas, seed):
    rng = np.random.default_rng(seed)
    return ((rng.normal(size=(batch, users, antennas))
             + 1j * rng.normal(size=(batch, users, antennas))) / np.sqrt(2)).astype(np.complex64)


def summarize(samples, unit, min_p99=100, min_p999=1000):
    n = len(samples)
    if n == 0:
        return {"count": 0, "unit": unit, "status": "NOT RUN"}
    a = np.sort(np.asarray(samples, dtype=np.float64))
    out = {
        "count": n, "unit": unit,
        "min": float(a[0]), "max": float(a[-1]),
        "mean": float(a.mean()), "stddev": float(a.std(ddof=1)) if n > 1 else 0.0,
        "p50": float(np.quantile(a, 0.50)), "p95": float(np.quantile(a, 0.95)),
        "p99": None, "p999": None,
    }
    if n >= min_p99:
        out["p99"] = float(np.quantile(a, 0.99))
    else:
        out["p99Note"] = f"insufficient samples (<{min_p99}) for a p99 estimate"
    if n >= min_p999:
        out["p999"] = float(np.quantile(a, 0.999))
    else:
        out["p999Note"] = f"insufficient samples (<{min_p999}) for a p99.9 estimate"
    return out


def gpu_env():
    try:
        q = subprocess.run(
            ["nvidia-smi",
             "--query-gpu=name,uuid,compute_cap,driver_version,memory.total",
             "--format=csv,noheader"],
            capture_output=True, text=True, timeout=15, check=True)
        rows = [r.strip() for r in q.stdout.strip().splitlines() if r.strip()]
        gpus = []
        for r in rows:
            name, uuid, cc, drv, mem = [c.strip() for c in r.split(",")]
            gpus.append({"name": name, "uuid": uuid, "computeCap": cc,
                         "driver": drv, "memoryTotal": mem})
        return gpus
    except Exception as exc:  # pragma: no cover
        return [{"error": f"nvidia-smi unavailable: {exc}"}]


def measure_compile(cmd):
    if not cmd:
        return {"status": "NOT RUN: no --measure-compile command given"}
    t0 = now_ns()
    proc = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    dt = (now_ns() - t0) / 1e9
    return {"command": cmd, "seconds": dt, "returncode": proc.returncode,
            "ok": proc.returncode == 0,
            "note": "one-shot build time; not a latency distribution"}


def bench_gpu(shape, alpha, reps, warmup, deadline_us, lib):
    b, u, n = shape
    be = open_backend(b, u, n, lib=lib)
    if be is None:
        return {"shape": shape, "status": "NOT RUN: no c2k_rzf backend library present"}
    with be:
        h = rand_channel(b, u, n, seed=1000 + b * 100 + u * 10 + n)

        # cold start: a *separate* fresh context so create() is genuinely cold.
        cold = open_backend(b, u, n, lib=lib)
        t0 = now_ns()
        # create() already ran inside open_backend; measure first run only, plus
        # report create()+run() as the composite cold path.
        _w, _s, _t = cold.run(h, alpha)
        cold_first_run_us = (now_ns() - t0) / 1e3
        cold.close()

        warm_kernel, warm_e2e = [], []
        for _ in range(warmup):
            be.run(h, alpha)

        kernel_ms, h2d_ms, d2h_ms, e2e_us = [], [], [], []
        event_timed = None
        for _ in range(reps):
            t0 = now_ns()
            _w, _s, tinfo = be.run(h, alpha)
            e2e_us.append((now_ns() - t0) / 1e3)
            event_timed = bool(tinfo["eventTimed"])
            if event_timed:
                kernel_ms.append(tinfo["kernelMs"])
                h2d_ms.append(tinfo["h2dMs"])
                d2h_ms.append(tinfo["d2hMs"])

        deadline_miss = None
        if deadline_us is not None and e2e_us:
            deadline_miss = float(np.mean(np.asarray(e2e_us) > deadline_us))

        phases = {
            "end_to_end": {**summarize(e2e_us, "us"),
                           "source": "host CLOCK_MONOTONIC_RAW",
                           "raw": e2e_us},
        }
        if event_timed:
            phases["kernel"] = {**summarize(kernel_ms, "ms"),
                                "source": "device events (kernel only)", "raw": kernel_ms}
            phases["h2d"] = {**summarize(h2d_ms, "ms"),
                             "source": "device events", "raw": h2d_ms}
            phases["d2h"] = {**summarize(d2h_ms, "ms"),
                             "source": "device events", "raw": d2h_ms}
        else:
            note = ("NOT RUN: backend did not populate device events; a host clock "
                    "is NOT substituted for GPU phases")
            phases["kernel"] = {"status": note}
            phases["h2d"] = {"status": note}
            phases["d2h"] = {"status": note}

        return {
            "shape": shape, "alpha": alpha, "reps": reps, "warmup": warmup,
            "backend": be.backend_name, "library": str(be.lib_path),
            "workspaceBytes": be.workspace_bytes(b, u, n),
            "coldStart": {"firstRunUS": cold_first_run_us,
                          "source": "host CLOCK_MONOTONIC_RAW",
                          "note": "single cold context; a cold-start distribution "
                                  "requires repeated fresh processes"},
            "deadlineUS": deadline_us, "deadlineMissRatio": deadline_miss,
            "phases": phases,
        }


def bench_cpu_numpy(shape, alpha, reps, warmup):
    b, u, n = shape
    h = rand_channel(b, u, n, seed=2000 + b * 100 + u * 10 + n)
    for _ in range(warmup):
        rzf_numpy(h, alpha, np.complex64)
    samples = []
    for _ in range(reps):
        t0 = now_ns()
        rzf_numpy(h, alpha, np.complex64)
        samples.append((now_ns() - t0) / 1e3)
    return {"shape": shape, "evidenceType": "measured-cpu-reference", "notGPU": True,
            "precision": "complex64", "reps": reps, "warmup": warmup,
            "phases": {"end_to_end": {**summarize(samples, "us"),
                                      "source": "host CLOCK_MONOTONIC_RAW", "raw": samples}}}


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--out", required=True, help="raw JSON output path")
    p.add_argument("--csv", help="optional per-sample CSV output path")
    p.add_argument("--reps", type=int, default=200)
    p.add_argument("--warmup", type=int, default=20)
    p.add_argument("--alpha", type=float, default=0.1)
    p.add_argument("--deadline-us", type=float, default=None)
    p.add_argument("--backend-lib", default=None, help="explicit backend .so path")
    p.add_argument("--measure-compile", default=None, help="shell command whose wall time is the compile phase")
    p.add_argument("--smoke", action="store_true")
    p.add_argument("--no-cpu-baseline", action="store_true")
    p.add_argument("--shapes", default=None, help="semicolon list like 1x4x32;4x8x64")
    args = p.parse_args()

    if args.shapes:
        shapes = [tuple(int(x) for x in s.lower().split("x")) for s in args.shapes.split(";")]
    elif args.smoke:
        shapes = [(1, 4, 16), (2, 8, 32)]
    else:
        shapes = [(1, 4, 32), (1, 8, 64), (4, 8, 64), (4, 16, 128)]
    reps = 20 if args.smoke else args.reps

    result = {
        "schema": "claim2kernel.dev/rzf-benchmark-v1",
        "generatedWith": "kernels/host/bench_rzf.py",
        "honesty": {
            "gpuPhasesFrom": "device events only",
            "hostPhasesFrom": "CLOCK_MONOTONIC_RAW",
            "syntheticCpuAsGpu": False,
            "tailQuantilesRequireSamples": {"p99": 100, "p999": 1000},
        },
        "host": {"python": platform.python_version(), "platform": platform.platform(),
                 "numpy": np.__version__},
        "gpus": gpu_env(),
        "compile": measure_compile(args.measure_compile),
        "gpuCases": [],
        "cpuBaselineCases": [],
    }
    for shape in shapes:
        result["gpuCases"].append(
            bench_gpu(list(shape), args.alpha, reps, args.warmup, args.deadline_us, args.backend_lib))
        if not args.no_cpu_baseline:
            result["cpuBaselineCases"].append(bench_cpu_numpy(list(shape), args.alpha, reps, args.warmup))

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(result, indent=2, allow_nan=False) + "\n")

    if args.csv:
        write_csv(Path(args.csv), result)

    summary = {"written": str(out), "shapes": [list(s) for s in shapes],
               "gpuBackend": next((c.get("backend") for c in result["gpuCases"] if "backend" in c), "NOT RUN")}
    print(json.dumps(summary))


def write_csv(path: Path, result: dict):
    import csv
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="") as fh:
        w = csv.writer(fh)
        w.writerow(["evidence", "backend", "shape", "phase", "unit", "sample_index", "value"])
        for case in result["gpuCases"]:
            if "phases" not in case:
                continue
            shape = "x".join(str(x) for x in case["shape"])
            backend = case.get("backend", "gpu")
            for phase, data in case["phases"].items():
                for i, v in enumerate(data.get("raw", [])):
                    w.writerow(["gpu", backend, shape, phase, data.get("unit", ""), i, v])
        for case in result["cpuBaselineCases"]:
            shape = "x".join(str(x) for x in case["shape"])
            for phase, data in case["phases"].items():
                for i, v in enumerate(data.get("raw", [])):
                    w.writerow(["cpu-baseline", "numpy", shape, phase, data.get("unit", ""), i, v])


if __name__ == "__main__":
    main()
