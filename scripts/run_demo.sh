#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
./scripts/build_demo_assets.sh >/dev/null
OUT="$ROOT/artifacts/demo"

"$OUT/c2k" select \
  --catalog "$OUT/catalog.json" \
  --request examples/requests/in-domain.json \
  --phase plan \
  --at 2026-08-20T03:00:00Z > "$OUT/plan-decision.json"

"$OUT/c2k" launch \
  --root "$OUT/runtime" \
  --profile "$OUT/profile-sealed.json" \
  --request examples/requests/in-domain.json \
  --metadata examples/dra/metadata-good.json \
  --dra-request gpu \
  --signature "$OUT/profile-signature.json" \
  --public-key "$OUT/keys/signing-public.pem" \
  --require-signature \
  --at 2026-08-20T03:00:00Z > "$OUT/launch-result.json"

"$OUT/c2k" render-k8s \
  --profile "$OUT/profile-sealed.json" \
  --request examples/requests/in-domain.json \
  --signature "$OUT/profile-signature.json" \
  --public-key "$OUT/keys/signing-public.pem" \
  --namespace default \
  --job c2k-demo \
  --image "ghcr.io/example/claim2kernel@sha256:$(printf '3%.0s' {1..64})" \
  --queue research \
  --driver cpu.example.com \
  --at 2026-08-20T03:00:00Z \
  --out "$OUT/kubernetes-list.json"

jq -e '.decision.admissible == true and .signatureValid == true and .executed == true and .exitCode == 0' "$OUT/launch-result.json" >/dev/null
jq -e '.items | length == 3' "$OUT/kubernetes-list.json" >/dev/null
printf '{"demo":"passed","plan":"%s","launch":"%s","manifest":"%s"}\n' "$OUT/plan-decision.json" "$OUT/launch-result.json" "$OUT/kubernetes-list.json"
