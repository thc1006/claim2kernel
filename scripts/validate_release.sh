#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
START=$(date -u +%Y-%m-%dT%H:%M:%SZ)
mkdir -p artifacts/validation
LOG=artifacts/validation/full.log
: > "$LOG"
run() { printf '\n$ %q ' "$1" >> "$LOG"; printf '%q ' "${@:2}" >> "$LOG"; printf '\n' >> "$LOG"; "$@" >> "$LOG" 2>&1; }
run gofmt -w cmd pkg kernels/demo
run go vet ./...
run go test ./...
run go test -race ./...
run env PYTHONPATH=tools/calibration python3 -m unittest discover -s tools/calibration/tests -v
run python3 -m compileall -q tools kernels/rzf
run python3 tools/schema_check.py
run bash -n scripts/adversarial_suite.sh scripts/build_demo_assets.sh scripts/calibrate_smoke.sh scripts/package.sh scripts/run_demo.sh scripts/validate_release.sh scripts/verify_package.sh kernels/mojo/build.sh
run ./scripts/run_demo.sh
run ./scripts/adversarial_suite.sh
run ./scripts/calibrate_smoke.sh
run python3 kernels/rzf/rzf_reference.py --self-test
run python3 kernels/rzf/benchmark.py --smoke --out artifacts/rzf-smoke.json
run timeout 30s env GOMAXPROCS=2 go test ./pkg/jsonsafe -run '^$' -fuzz FuzzDecodeStrict -fuzztime 3s
run timeout 30s env GOMAXPROCS=2 go test ./pkg/dra -run '^$' -fuzz FuzzDecodeMetadataStream -fuzztime 3s

MOJO_STATUS='NOT RUN: mojo executable and supported GPU were unavailable in this environment.'
if command -v mojo >/dev/null 2>&1; then
  if ./kernels/mojo/build.sh artifacts/mojo >> "$LOG" 2>&1; then MOJO_STATUS='PASSED on available accelerator'; else MOJO_STATUS='FAILED; inspect artifacts/validation/full.log'; exit 1; fi
fi
DOCKER_STATUS='NOT RUN: docker executable was unavailable in this environment.'
if command -v docker >/dev/null 2>&1; then
  run docker build -t claim2kernel:validation .
  DOCKER_STATUS='PASSED'
fi
END=$(date -u +%Y-%m-%dT%H:%M:%SZ)
GO_VERSION=$(go version)
PY_VERSION=$(python3 --version)
CASES=$(wc -l < artifacts/adversarial-results.tsv | tr -d ' ')
cat > VALIDATION_REPORT.md <<EOF
# Validation report

- Started: $START
- Completed: $END
- Go: $GO_VERSION
- Python: $PY_VERSION
- Unit tests: passed
- Race detector: passed
- Python statistical tests: passed
- End-to-end signed launch: passed
- Adversarial cases: $CASES passed
- Stateful invariants: passed for valid trace; all five negative traces rejected
- Fuzz smoke: 3 seconds each for strict JSON and DRA metadata decoders
- RZF complex128/complex64 oracle smoke: passed
- Mojo/GPU: $MOJO_STATUS
- Container build: $DOCKER_STATUS

This report is evidence about the reference implementation in this environment.
It is **not** evidence of Mojo GPU performance, tail-latency coverage on production
hardware, O-RAN conformance, or hard real-time behavior. Those claims require the
self-hosted hardware protocol in the repository and independently retained raw
measurements.

Full command log: \`artifacts/validation/full.log\` (generated locally and excluded
from release archives).
EOF
printf '{"validation":"passed","report":"%s","adversarialCases":%s}\n' "$ROOT/VALIDATION_REPORT.md" "$CASES"
