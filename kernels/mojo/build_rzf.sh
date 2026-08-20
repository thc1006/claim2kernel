#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Build the RZF Mojo kernel for a declared target accelerator and retain logs.
#
# Verified working with Mojo 1.0.0 / MAX 26.5 (in the pinned >=1.0.0b2,<1.1.0
# window). `mojo` must be on PATH from a MAX-enabled install (the GPU host APIs
# live in the `max` package, not the bare `mojo` package). Cross-compiles the
# kernel to the target's PTX/GCN at BUILD time (AOT); no device is needed to
# COMPILE, but EXECUTION requires a matching GPU and is reported separately.
#
# Note: --Werror is intentionally NOT used here. The kernel currently compiles
# with pointer-API deprecation warnings (UnsafePointer -> Pointer, ptr[i] ->
# unsafe_offset=) that are warnings under Mojo 1.0.0; migrating them is tracked
# as follow-up. The build still fails on any real error.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
OUT=${1:-"$ROOT/artifacts/mojo"}
TARGET=${MOJO_TARGET_ACCELERATOR:-nvidia:sm_90}
mkdir -p "$OUT"

python3 "$ROOT/scripts/check_mojo_version.py" "$(mojo --version 2>&1)"
mojo --version | tee "$OUT/mojo-version.txt"

SAFE_TARGET=${TARGET//[:]/_}
echo "building RZF kernel for target: $TARGET"
START=$(date +%s.%N)
mojo build --target-accelerator "$TARGET" \
  "$ROOT/kernels/mojo/rzf_kernel.mojo" \
  -o "$OUT/rzf-conformance-$SAFE_TARGET" 2>&1 | tee "$OUT/rzf-build-$SAFE_TARGET.log"
END=$(date +%s.%N)
SECS=$(awk "BEGIN{printf \"%.3f\", $END-$START}")

# Inspectable assembly (host + embedded kernel) for the same target.
mojo build --target-accelerator "$TARGET" --emit asm \
  "$ROOT/kernels/mojo/rzf_kernel.mojo" \
  -o "$OUT/rzf-$SAFE_TARGET.s" 2>&1 | tee -a "$OUT/rzf-build-$SAFE_TARGET.log" || true

sha256sum "$OUT/rzf-conformance-$SAFE_TARGET" | tee "$OUT/rzf-conformance-$SAFE_TARGET.sha256"
printf '{"mojoBuild":"ok","target":"%s","seconds":%s,"artifact":"%s","execution":"NOT RUN unless a matching %s device is present"}\n' \
  "$TARGET" "$SECS" "$OUT/rzf-conformance-$SAFE_TARGET" "$TARGET"
