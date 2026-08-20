#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
OUT=${1:-"$ROOT/artifacts/mojo"}
TARGET=${MOJO_TARGET_ACCELERATOR:-}
mkdir -p "$OUT"
python3 "$ROOT/scripts/check_mojo_version.py" "$(mojo --version 2>&1)"
COMMON=(--Werror --warn-on-unstable-apis)
if [[ -n "$TARGET" ]]; then COMMON+=(--target-accelerator "$TARGET"); fi
mojo build "${COMMON[@]}" "$ROOT/kernels/mojo/gpu_conformance.mojo" -o "$OUT/gpu-conformance"
mojo build "${COMMON[@]}" --emit shared-lib "$ROOT/kernels/mojo/contract_exports.mojo" -o "$OUT/libc2k_contract.so"
mojo build "${COMMON[@]}" --emit asm "$ROOT/kernels/mojo/gpu_conformance.mojo" -o "$OUT/gpu-conformance.s"
"$OUT/gpu-conformance"
printf '{"mojoBuild":"passed","target":"%s"}\n' "${TARGET:-host-detected}"
