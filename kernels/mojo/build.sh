#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
OUT=${1:-"$ROOT/artifacts/mojo"}
TARGET=${MOJO_TARGET_ACCELERATOR:-}
mkdir -p "$OUT"
python3 "$ROOT/scripts/check_mojo_version.py" "$(mojo --version 2>&1)"
# --Werror is intentionally omitted: Mojo 1.0.0 emits non-fatal deprecation
# warnings (ABI="C" -> abi("C"), UnsafePointer -> Pointer) that migrating is
# tracked as follow-up; a real error still fails the build.
COMMON=(--warn-on-unstable-apis)
if [[ -n "$TARGET" ]]; then COMMON+=(--target-accelerator "$TARGET"); fi
mojo build "${COMMON[@]}" "$ROOT/kernels/mojo/gpu_conformance.mojo" -o "$OUT/gpu-conformance"
mojo build "${COMMON[@]}" --emit shared-lib "$ROOT/kernels/mojo/contract_exports.mojo" -o "$OUT/libc2k_contract.so"
mojo build "${COMMON[@]}" --emit asm "$ROOT/kernels/mojo/gpu_conformance.mojo" -o "$OUT/gpu-conformance.s"
"$OUT/gpu-conformance"
printf '{"mojoBuild":"passed","target":"%s"}\n' "${TARGET:-host-detected}"

# The RZF precoder kernel is built by kernels/mojo/build_rzf.sh, which
# cross-compiles kernels/mojo/rzf_kernel.mojo for a declared target accelerator
# (nvidia:sm_90 / nvidia:sm_80 / amdgpu:gfx942) and retains build logs + digests.
# See kernels/mojo/RZF_EVIDENCE.md for the verified compile results.
