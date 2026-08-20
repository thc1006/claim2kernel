#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
OUT="$ROOT/artifacts/demo"
RUNTIME="$OUT/runtime"
rm -rf "$OUT"
mkdir -p "$RUNTIME/bin" "$OUT/keys"

GO_VERSION=$(go version | awk '{print $3}' | sed 's/^go//')
go build -trimpath -o "$OUT/c2k" ./cmd/c2k
go build -trimpath -o "$RUNTIME/bin/demo-kernel" ./kernels/demo
chmod 0500 "$RUNTIME/bin/demo-kernel"

python3 tools/calibration/bind_artifact.py \
  --profile examples/profile-demo-template.json \
  --artifact "$RUNTIME/bin/demo-kernel" \
  --root "$RUNTIME" \
  --source kernels/demo/main.go \
  --dataset examples/calibration/demo-protocol-cases.json \
  --compiler "go${GO_VERSION}" \
  --out "$OUT/profile-bound.json"

"$OUT/c2k" seal \
  --profile "$OUT/profile-bound.json" \
  --out "$OUT/profile-sealed.json" \
  --at 2026-08-20T01:00:00Z
"$OUT/c2k" keygen \
  --private "$OUT/keys/signing-private.pem" \
  --public "$OUT/keys/signing-public.pem" > "$OUT/keygen.json"
"$OUT/c2k" sign \
  --profile "$OUT/profile-sealed.json" \
  --private-key "$OUT/keys/signing-private.pem" \
  --out "$OUT/profile-signature.json" \
  --at 2026-08-20T02:00:00Z

python3 - "$OUT/profile-sealed.json" "$OUT/catalog.json" <<'PY'
import json,sys
from pathlib import Path
profile=json.loads(Path(sys.argv[1]).read_text())
catalog={"apiVersion":"claim2kernel.dev/v1alpha1","kind":"KernelCatalog","metadata":{"name":"demo-catalog"},"profiles":[profile]}
Path(sys.argv[2]).write_text(json.dumps(catalog,indent=2,allow_nan=False)+"\n")
PY

"$OUT/c2k" validate --profile "$OUT/profile-sealed.json" --sealed >/dev/null
"$OUT/c2k" validate --signature "$OUT/profile-signature.json" >/dev/null
"$OUT/c2k" validate --catalog "$OUT/catalog.json" --sealed >/dev/null
printf '{"built":true,"output":"%s","compiler":"go%s"}\n' "$OUT" "$GO_VERSION"
