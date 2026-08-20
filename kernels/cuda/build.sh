#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Build the CUDA RZF reference/baseline shared library (implements c2k_rzf.h).
#
# This is a REFERENCE/BASELINE builder. The architecture is auto-detected from
# the local GPU unless C2K_CUDA_ARCH overrides it. On the CI/dev host here that
# is sm_61 (GTX 1050), which is NOT a declared target -- see RZF_EVIDENCE.md.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
OUT=${1:-"$ROOT/artifacts/rzf"}
mkdir -p "$OUT"

ARCH="${C2K_CUDA_ARCH:-}"
if [[ -z "$ARCH" ]]; then
  CAP=$(nvidia-smi --query-gpu=compute_cap --format=csv,noheader 2>/dev/null | head -1 | tr -d ' .') || true
  [[ -n "${CAP:-}" ]] && ARCH="sm_${CAP}"
fi
ARCH="${ARCH:-sm_70}"

echo "building CUDA RZF baseline for ${ARCH} (override with C2K_CUDA_ARCH)"
nvcc --version | tee "$OUT/cuda-nvcc-version.txt"
nvidia-smi --query-gpu=name,compute_cap,driver_version --format=csv,noheader \
  | tee "$OUT/cuda-gpu.txt" || true

START=$(date +%s.%N)
nvcc -std=c++17 -O3 -arch="$ARCH" --shared -Xcompiler -fPIC \
  -I "$ROOT/kernels/include" "$ROOT/kernels/cuda/rzf_cuda.cu" \
  -o "$OUT/libc2k_rzf_cuda.so" 2>&1 | tee "$OUT/cuda-build.log"
END=$(date +%s.%N)
SECS=$(awk "BEGIN{printf \"%.3f\", $END-$START}")

sha256sum "$OUT/libc2k_rzf_cuda.so" | tee "$OUT/libc2k_rzf_cuda.so.sha256"
printf '{"cudaBuild":"ok","arch":"%s","seconds":%s,"lib":"%s"}\n' \
  "$ARCH" "$SECS" "$OUT/libc2k_rzf_cuda.so"
