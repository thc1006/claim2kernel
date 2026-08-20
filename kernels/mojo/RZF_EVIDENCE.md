# RZF kernel: evidence and NOT RUN ledger

This document is the honest record of what was and was not executed while
building the batched complex regularized zero-forcing (RZF) precoder for
Claim2Kernel / MojoRAN. It follows the repository rule that no Mojo/GPU target
result is claimed without retained measurements, and that everything not actually
executed is marked **NOT RUN** with the exact command to produce it on the
self-hosted hardware protocol (`docs/MOJO_HARDWARE_PROTOCOL.md`).

## 0. One-paragraph honesty statement

The RZF *algorithm* (regularized Hermitian Gram, Cholesky, triangular solves,
Frobenius normalization; no explicit inverse) is validated on CPU against the
complex128 oracle to ~1e-16, and validated on **real GPU silicon** through a
byte-for-byte CUDA mirror. That CUDA run happened on this host's **NVIDIA GeForce
GTX 1050 (Pascal, sm_61)** which is **NOT** a declared target (NVIDIA sm_80/
sm_90, AMD gfx942) and is **NOT** the Mojo kernel -- it is a methodology, ABI,
and algorithm check only. The Mojo kernel **COMPILES** for all three declared
targets (nvidia:sm_90, nvidia:sm_80, amdgpu:gfx942) with Mojo 1.0.0 / MAX 26.5;
real build artifacts, logs, and digests are under `artifacts/mojo/`. Mojo GPU
**EXECUTION is NOT RUN**: there is no sm_80/sm_90/gfx942 device here, and the
local sm_61 GPU is rejected by the toolchain's own `ptxas`
(`Value 'sm_61' is not defined for option 'gpu-name'`). No target-architecture
Mojo performance, latency, or coverage is claimed anywhere.

## 1. Environment (exact)

| Component | Value |
|---|---|
| OS / kernel | Linux 7.0.0-28-generic |
| CPU toolchain | gcc 13.3.0 |
| Go | go1.26.5 linux/amd64 |
| Python | system 3.12.3 (no NumPy); venv Python 3.12 + NumPy 2.5.2 |
| CUDA | nvcc 12.0, V12.0.140 (ptxas supports sm_61) |
| Mojo / MAX | **Mojo 1.0.0 (ed45d567) / MAX 26.5.0** (installed via pixi; in the pinned >=1.0.0b2,<1.1.0 window). Its bundled ptxas supports sm_80+ only. |
| GPU | NVIDIA GeForce GTX 1050 -- compute cap **6.1 (Pascal, sm_61)**, driver 580.173.02, 3072 MiB |
| Docker | 29.4.0 |
| AMD ROCm | absent (no AMD device) |

## 2. Deliverable status

| Deliverable | Status | Evidence / reason |
|---|---|---|
| Mojo RZF GPU kernel source | **RUN (delivered + compiles)** | `kernels/mojo/rzf_kernel.mojo` |
| Mojo kernel compile: nvidia:sm_90 | **RUN** | `artifacts/mojo/rzf-conformance-nvidia_sm_90` (+ .sha256, build log), ~1.0 s |
| Mojo kernel compile: nvidia:sm_80 | **RUN** | `artifacts/mojo/rzf-conformance-nvidia_sm_80` (+ .sha256, build log) |
| Mojo kernel compile: amdgpu:gfx942 | **RUN** | `artifacts/mojo/rzf-conformance-amdgpu_gfx942` (+ .sha256, build log) |
| Mojo kernel compile: local sm_61 | **NOT RUN (unsupported)** | toolchain ptxas: `Value 'sm_61' is not defined` |
| Mojo kernel GPU execution (any target) | **NOT RUN** | no sm_80/sm_90/gfx942 device; sm_61 not buildable |
| C ABI / stable host interface | **RUN** | `kernels/include/c2k_rzf.h`, exercised on GPU via the CUDA backend + `kernels/host/rzf_host.py` |
| CPU/NumPy oracle comparison | **RUN** | `kernels/rzf/rzf_algorithm.py`: relL2 5.2e-16 (c128), 1.7e-7 (c64) vs oracle |
| Correctness tests (all cases) | **RUN** | `kernels/rzf/tests/test_rzf_correctness.py`: 21/21 pass, CPU spec + real GPU parity |
| CUDA baseline build + run | **RUN (sm_61 baseline)** | `kernels/cuda/rzf_cuda.cu` -> `artifacts/rzf/libc2k_rzf_cuda.so`; relL2 2.3e-7 vs oracle |
| Benchmark: compile time | **RUN** | `artifacts/rzf/bench-gtx1050.json` `.compile` (nvcc ~1.6 s) |
| Benchmark: cold-start | **RUN (sm_61)** | first create()+run() host wall per case |
| Benchmark: warmed kernel-only | **RUN (sm_61, device events)** | e.g. 4x8x64 kernel p50 ~0.039 ms, p99 ~0.041 ms (n=300) |
| Benchmark: H2D / D2H | **RUN (sm_61, device events)** | separated CUDA-event timings in JSON/CSV |
| Benchmark: end-to-end | **RUN (sm_61, host raw clock)** | CLOCK_MONOTONIC_RAW; e.g. 4x8x64 p50 ~80 us, p99 ~108 us |
| Benchmark: Mojo device timings | **NOT RUN** | Mojo backend reports `event_timed=0`; never faked from host wall |
| Raw JSON/CSV schema | **RUN** | `schemas/rzf-benchmark-v1.schema.json`; outputs in `artifacts/rzf/` |
| NumPy baseline comparison | **RUN** | `kernels/host/compare.py` -> `artifacts/rzf/comparison.json` |
| CUDA GPU comparison | **RUN (sm_61)** | same file, `c2k-rzf-cuda` rows |
| cuSOLVER/rocSOLVER (torch) baseline | **NOT RUN** | `kernels/baselines/rzf_torch.py`; no torch, sm_61 below wheel arch |
| Triton / HIP baseline | **NOT RUN** | no Triton; no AMD device (hipify `rzf_cuda.cu` on gfx942) |
| stdin-json-v1 artifact service | **RUN (sm_61)** | `kernels/host/rzf_service.py` piped end-to-end on GPU |
| Source + dataset digest binding | **RUN** | `artifacts/rzf/profile-rzf-bound.json` (unsealed) |
| Artifact/container/compilerDigest binding | **NOT RUN** | no deployable Mojo artifact/container; compiler recorded as Mojo 1.0.0 / MAX 26.5 |
| Profile seal + Ed25519 signature | **NOT RUN** | requires the above digests; profile intentionally UNSEALED |
| Statistical latency calibration (target HW) | **NOT RUN** | requires target-GPU grouped train/cal/test with a frozen gate |
| Container image build | **NOT RUN** | requires a Mojo base image |

## 3. What RUN means here (reproduce)

CPU/oracle/CUDA path needs a NumPy interpreter
(`pip install -r tools/calibration/requirements-dev.txt`):

```bash
PYTHONPATH=kernels/rzf python kernels/rzf/rzf_algorithm.py         # CPU vs c128 oracle
./kernels/cuda/build.sh artifacts/rzf                             # CUDA baseline (auto sm_61 here)
C2K_RZF_LIB=artifacts/rzf/libc2k_rzf_cuda.so PYTHONPATH=kernels/rzf:kernels/host \
  python -m unittest -v kernels.rzf.tests.test_rzf_correctness    # 21 cases, CPU + real GPU
python kernels/host/bench_rzf.py --out artifacts/rzf/bench-gtx1050.json \
  --csv artifacts/rzf/bench-gtx1050.csv --reps 300 --warmup 30 --deadline-us 2000 \
  --backend-lib artifacts/rzf/libc2k_rzf_cuda.so \
  --measure-compile "./kernels/cuda/build.sh artifacts/rzf"
python kernels/host/compare.py --out artifacts/rzf/comparison.json \
  --backend-lib artifacts/rzf/libc2k_rzf_cuda.so
```

Mojo kernel compile (needs a MAX-enabled `mojo` on PATH; GPU host APIs live in
the `max` package):

```bash
MOJO_TARGET_ACCELERATOR=nvidia:sm_90  ./kernels/mojo/build_rzf.sh artifacts/mojo
MOJO_TARGET_ACCELERATOR=nvidia:sm_80  ./kernels/mojo/build_rzf.sh artifacts/mojo
MOJO_TARGET_ACCELERATOR=amdgpu:gfx942 ./kernels/mojo/build_rzf.sh artifacts/mojo
```

## 4. What NOT RUN means here (produce on the hardware runner)

On a self-hosted sm_80/sm_90 (or gfx942) host with an in-window Mojo:

```bash
MOJO_TARGET_ACCELERATOR=nvidia:sm_90 ./kernels/mojo/build_rzf.sh artifacts/mojo/h100
# run the compiled conformance executable on the matching device to execute
# the kernel and verify per-batch ||W[b]||_F == 1; wire c2k_rzf_run device-event
# timing so timing.event_timed=1 (until then the harness records Mojo kernel/H2D/
# D2H as NOT RUN rather than substituting host wall time).
```

Then run the vendor-library baseline (`kernels/baselines/rzf_torch.py`, cuSOLVER
on NVIDIA / rocSOLVER on AMD) under matched rules, collect grouped calibration
data, pass the frozen independent test gate, build the deployable artifact +
container, and only then `c2k seal` + `c2k sign`.

## 5. Mojo install and compile result

A `pixi` install fetched **Mojo 1.0.0 / MAX 26.5.0** (the `max` conda package;
the GPU host APIs -- `DeviceContext`, `barrier` -- live there, not in the bare
`mojo` package). `scripts/check_mojo_version.py` accepts 1.0.0 (it is inside
`>=1.0.0b2,<1.1.0`). The kernel compiles cleanly for nvidia:sm_90, nvidia:sm_80,
and amdgpu:gfx942 (build logs in `artifacts/mojo/rzf-build-*.log`), emitting
target PTX/GCN via the toolchain's `ptxas`/`lld`. The only compile that fails is
the local sm_61, which the bundled `ptxas` does not define -- direct evidence
that this GPU is not a Mojo target. The source compiles with pointer-API
deprecation warnings (`UnsafePointer`->`Pointer`, positional `ptr[i]`->
`unsafe_offset=`); migrating them is tracked follow-up and `build_rzf.sh` omits
`--Werror` for that reason.

## 6. The GTX 1050 caveat (read this before citing any number)

Every GPU *latency/throughput* number in `artifacts/rzf/*.json|csv` was measured
on a **GTX 1050 (sm_61)** running the **CUDA baseline**, not the Mojo kernel and
not a target GPU. Those numbers validate correctness, the C ABI, determinism,
fail-closed behavior, and the benchmark methodology. The Mojo `artifacts/mojo/*`
are **compile-only** artifacts for sm_80/sm_90/gfx942 and carry no runtime
numbers. Neither set may appear in a paper or profile as sm_80/sm_90/gfx942 Mojo
performance. Device name, UUID, compute capability, and driver are embedded in
every benchmark JSON so provenance cannot be mistaken.
