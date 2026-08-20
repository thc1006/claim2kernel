#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
OUT="$ROOT/artifacts/calibration"
rm -rf "$OUT"
mkdir -p "$OUT"

python3 kernels/rzf/generate_smoke_dataset.py --out "$OUT/rzf-synthetic-ci-only.csv" >/dev/null
SOURCE_DIGEST="sha256:$(sha256sum kernels/rzf/rzf_reference.py | awk '{print $1}')"
PYTHONPATH=tools/calibration python3 tools/calibration/calibrate.py \
  --template examples/profile-rzf-template.json \
  --csv "$OUT/rzf-synthetic-ci-only.csv" \
  --out "$OUT/rzf-synthetic-unsealed.json" \
  --report "$OUT/rzf-synthetic-report.json" \
  --method one-sided-tolerance \
  --coverage 0.95 \
  --confidence 0.95 \
  --compiler "SYNTHETIC-CI-NOT-MOJO" \
  --source-digest "$SOURCE_DIGEST" \
  --calibrated-at 2026-08-20T00:00:00Z \
  --max-age-seconds 2592000 \
  --io-budget-us 50 \
  --runtime-jitter-us 50

go build -trimpath -o "$OUT/c2k" ./cmd/c2k
"$OUT/c2k" validate --profile "$OUT/rzf-synthetic-unsealed.json" >/dev/null
jq -e '.claims.hardRealtime == false and .claims.testUsedForFitting == false and .latency.independentObservedCoverage >= .latency.coverageTarget and .ood.independentInlierRate >= .latency.coverageTarget' "$OUT/rzf-synthetic-report.json" >/dev/null
printf '{"calibrationSmoke":"passed","warning":"synthetic CI data; not performance evidence","report":"%s"}\n' "$OUT/rzf-synthetic-report.json"
