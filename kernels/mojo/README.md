# Mojo hardware path

This directory is pinned to the **currently verified public release line,
Mojo 1.0.0b2**. It is not presented as proof of GPU performance until the
self-hosted hardware workflow publishes its raw logs and artifacts.

Run on a supported NVIDIA or AMD host:

```bash
MOJO_TARGET_ACCELERATOR=nvidia:sm_90 ./kernels/mojo/build.sh
# or
MOJO_TARGET_ACCELERATOR=amdgpu:gfx942 ./kernels/mojo/build.sh
```

The conformance executable checks allocation, host/device copies, kernel
launch, synchronization, and every output element. `contract_exports.mojo`
provides a versioned C ABI scaffold for a future optimized RZF implementation.
The repository's Python complex128 RZF implementation remains the correctness
oracle; CUDA/HIP/Triton/Mojo implementations must be compared against it.
