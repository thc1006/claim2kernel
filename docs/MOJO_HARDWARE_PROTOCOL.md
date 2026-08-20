# Mojo hardware validation protocol

## Status pin

As of 2026-08-20, the verified public line is Mojo **1.0 Beta 2** in Modular
26.4. The project accepts `>=1.0.0b2,<1.1.0`. This is not a claim that final
Mojo 1.0 has shipped.

## Target examples

```bash
MOJO_TARGET_ACCELERATOR=nvidia:sm_80 ./kernels/mojo/build.sh artifacts/mojo/a100
MOJO_TARGET_ACCELERATOR=nvidia:sm_90 ./kernels/mojo/build.sh artifacts/mojo/h100
MOJO_TARGET_ACCELERATOR=amdgpu:gfx942 ./kernels/mojo/build.sh artifacts/mojo/mi300x
```

The self-hosted runner must pin and record:

- physical GPU model, UUID or stable lab pseudonym, partition/profile;
- CPU, NUMA, memory, PCIe topology;
- OS/kernel, container runtime, Kubernetes, Kueue;
- GPU driver and firmware/runtime versions;
- Mojo/MAX version and compiler digest when obtainable;
- container image and kernel artifact digests;
- power/clock policy and thermal state;
- all feature gates and DRA driver versions.

## Required phases

1. **Conformance**: compile the included GPU probe, execute it, and verify every
   output element.
2. **Correctness**: compare each Mojo RZF output with the complex128 Python
   oracle over deterministic and randomized matrices, including ill-conditioned
   cases.
3. **Warm-up determination**: preregister how warm-up iterations are excluded;
   retain them separately.
4. **Calibration**: collect grouped train/calibration data without test access.
5. **Frozen test**: run the independent test groups once for a candidate release.
6. **Contention**: repeat under controlled idle, compute, memory-bandwidth, and
   transfer pressure within and outside the declared envelope.
7. **Lifecycle**: driver/container restart, DRA reallocation, partition change,
   profile revocation, and metadata drift.
8. **Cross-stack baselines**: tuned CUDA/HIP/Triton/vendor library, CPU BLAS, and
   static Mojo whole-device paths as applicable.

## Sampling rules

- Use `CLOCK_MONOTONIC_RAW` or device events appropriate to the backend.
- Separate host-to-device, kernel, device-to-host, and end-to-end latency.
- Retain raw per-sample data; never report only averages.
- Report p50/p95/p99/p99.9, confidence/tolerance construction, deadline miss
  ratio, throughput, memory, utilization, energy when available, and numerical
  error.
- Group by independent run/time block/device configuration. Never split samples
  from one autocorrelated run across train/calibration/test.
- Record failed runs and timeouts rather than deleting them.

## Claim gate

A Mojo/GPU performance claim is permitted only when:

- compiler and target build logs are retained;
- source, artifact, container, and dataset digests are bound into the profile;
- the independent test gate passes without post-hoc tuning;
- all baselines use equivalent input, precision, correctness, and warm-up rules;
- raw data and scripts reproduce every figure;
- limitations and negative results are reported.
