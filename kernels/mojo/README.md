# Mojo hardware path

This directory is pinned to the currently verified public release line,
**Mojo 1.0.0 / MAX 26.5** (accepted by `scripts/check_mojo_version.py`, which
enforces `>=1.0.0b2,<1.1.0`). Nothing here is presented as proof of GPU
performance until the self-hosted hardware workflow publishes raw logs and
artifacts (`docs/MOJO_HARDWARE_PROTOCOL.md`).

## Files

- `rzf_kernel.mojo` -- the batched complex RZF precoder GPU kernel (the
  deployable target) plus the `c2k_rzf` C ABI scalar exports and a DeviceContext
  conformance `main`. One thread block per batch element; regularized Hermitian
  Gram + in-place Cholesky + triangular solves + Frobenius normalization, all in
  FP32, forming no explicit inverse. Fail-closed per-batch status.
- `../include/c2k_rzf.h` -- the stable C ABI shared by the Mojo kernel and the
  CUDA reference (`../cuda/rzf_cuda.cu`), so `../host/rzf_host.py` can drive
  either backend unchanged.
- `build_rzf.sh` -- cross-compiles `rzf_kernel.mojo` for a declared target and
  retains logs + digests.
- `gpu_conformance.mojo`, `contract_exports.mojo` -- the original probe/ABI
  scaffold (older API surface).
- `RZF_EVIDENCE.md` -- the RUN / NOT RUN ledger with exact versions.

## Build the RZF kernel

`mojo` must be on PATH from a MAX-enabled install (the GPU host APIs -- the
`barrier` primitive and `DeviceContext` -- live in the `max` package, not the
bare `mojo` package):

```bash
MOJO_TARGET_ACCELERATOR=nvidia:sm_90  ./kernels/mojo/build_rzf.sh
MOJO_TARGET_ACCELERATOR=nvidia:sm_80  ./kernels/mojo/build_rzf.sh
MOJO_TARGET_ACCELERATOR=amdgpu:gfx942 ./kernels/mojo/build_rzf.sh
```

The kernel is cross-compiled to the target's PTX/GCN at build time (AOT); the
request hot path enqueues the pre-compiled function (`compile_function` once,
`enqueue_function` per launch) and does not JIT. Compiling needs no device;
**executing** needs a matching sm_80/sm_90/gfx942 GPU and is a separate,
independently recorded step.

## Correctness authority

The Python complex128 RZF implementation (`../rzf/rzf_reference.py`) remains the
oracle; `../rzf/rzf_algorithm.py` is the explicit-Cholesky CPU mirror the GPU
kernels transliterate. CUDA/HIP/Mojo outputs must be compared against the oracle
(`../rzf/tests/test_rzf_correctness.py`, `../host/compare.py`).
