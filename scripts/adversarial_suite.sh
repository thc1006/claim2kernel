#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
./scripts/build_demo_assets.sh >/dev/null
OUT="$ROOT/artifacts/demo"
REPORT="$ROOT/artifacts/adversarial-results.tsv"
: > "$REPORT"
COUNT=0

pass() { COUNT=$((COUNT+1)); printf 'PASS\t%s\n' "$1" >> "$REPORT"; }
expect_fail() {
  local name=$1 needle=$2; shift 2
  local log="$OUT/adversarial-${name}.log"
  if "$@" >"$log" 2>&1; then
    printf 'expected failure: %s\n' "$name" >&2; cat "$log" >&2; exit 1
  fi
  if ! grep -Fq "$needle" "$log"; then
    printf 'failure %s did not contain %q\n' "$name" "$needle" >&2; cat "$log" >&2; exit 1
  fi
  pass "$name"
}

expect_fail duplicate-key 'duplicate JSON object key' "$OUT/c2k" validate --profile examples/adversarial/profile-duplicate-key.json
expect_fail reserved-fallback 'reserved in v1alpha1' "$OUT/c2k" validate --profile examples/adversarial/profile-reserved-fallback.json
expect_fail inexact-integer 'exact v1alpha1 range' "$OUT/c2k" validate --request examples/adversarial/request-inexact-integer.json
expect_fail ood-rejected 'OOD_REJECTED' "$OUT/c2k" select --catalog "$OUT/catalog.json" --request examples/requests/ood.json --phase plan --at 2026-08-20T03:00:00Z
expect_fail hard-range 'INPUT_OUT_OF_RANGE' "$OUT/c2k" select --catalog "$OUT/catalog.json" --request examples/requests/hard-range.json --phase plan --at 2026-08-20T03:00:00Z
expect_fail unseen-category 'UNSEEN_CATEGORY' "$OUT/c2k" select --catalog "$OUT/catalog.json" --request examples/requests/unseen-category.json --phase plan --at 2026-08-20T03:00:00Z
expect_fail unknown-input 'UNKNOWN_INPUT' "$OUT/c2k" select --catalog "$OUT/catalog.json" --request examples/requests/unknown-input.json --phase plan --at 2026-08-20T03:00:00Z
expect_fail version-mismatch 'UNSUPPORTED_VERSION' "$OUT/c2k" select --catalog "$OUT/catalog.json" --request examples/requests/version-mismatch.json --phase plan --at 2026-08-20T03:00:00Z
expect_fail interference-envelope 'INTERFERENCE_OUT_OF_ENVELOPE' "$OUT/c2k" select --catalog "$OUT/catalog.json" --request examples/requests/interference-ood.json --phase plan --at 2026-08-20T03:00:00Z
expect_fail tight-deadline 'LATENCY_SLO_UNSATISFIED' "$OUT/c2k" select --catalog "$OUT/catalog.json" --request examples/requests/tight-deadline.json --phase plan --at 2026-08-20T03:00:00Z
expect_fail relation-violation 'RELATION_VIOLATION' "$OUT/c2k" select --catalog "$OUT/catalog.json" --request examples/requests/relation-violation.json --phase plan --at 2026-08-20T03:00:00Z

for pair in \
  'bad-arch|examples/dra/metadata-bad-architecture.json|DRA_ATTRIBUTE_MISMATCH' \
  'missing-health|examples/dra/metadata-missing-health.json|DRA_ATTRIBUTE_MISSING' \
  'extra-device|examples/dra/metadata-extra-device.json|DRA_DEVICE_COUNT_MISMATCH'; do
  IFS='|' read -r name meta needle <<<"$pair"
  expect_fail "$name" "$needle" "$OUT/c2k" launch --verify-only --root "$OUT/runtime" --profile "$OUT/profile-sealed.json" --request examples/requests/in-domain.json --metadata "$meta" --signature "$OUT/profile-signature.json" --public-key "$OUT/keys/signing-public.pem" --require-signature --at 2026-08-20T03:00:00Z
done

"$OUT/c2k" inspect-metadata --file examples/adversarial/metadata-unknown-then-valid.json >/dev/null && pass metadata-version-forward-compat
expect_fail metadata-unknown-field 'unknown field' "$OUT/c2k" inspect-metadata --file examples/adversarial/metadata-unknown-field.json
expect_fail metadata-union-confusion 'exactly one union member' "$OUT/c2k" inspect-metadata --file examples/adversarial/metadata-union-confusion.json

cp "$OUT/profile-sealed.json" "$OUT/tampered-profile.json"
python3 - "$OUT/tampered-profile.json" <<'PY'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]);obj=json.loads(p.read_text());obj['spec']['latency']['ioBudgetUS']+=1;p.write_text(json.dumps(obj,indent=2)+'\n')
PY
expect_fail seal-tamper 'contract digest mismatch' "$OUT/c2k" validate --profile "$OUT/tampered-profile.json" --sealed

python3 - "$OUT/tampered-profile.json" <<'PY'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]);obj=json.loads(p.read_text());obj.pop('seal',None);p.write_text(json.dumps(obj,indent=2)+'\n')
PY
"$OUT/c2k" seal --profile "$OUT/tampered-profile.json" --out "$OUT/resealed-tampered.json" --at 2026-08-20T01:30:00Z
expect_fail old-signature-on-new-contract 'different profile or artifact' "$OUT/c2k" verify-signature --profile "$OUT/resealed-tampered.json" --signature "$OUT/profile-signature.json" --public-key "$OUT/keys/signing-public.pem" --at 2026-08-20T03:00:00Z

cp "$OUT/runtime/bin/demo-kernel" "$OUT/runtime/bin/demo-kernel-tampered"
printf 'x' >> "$OUT/runtime/bin/demo-kernel"
expect_fail artifact-tamper 'ARTIFACT_VERIFICATION_FAILED' "$OUT/c2k" launch --verify-only --root "$OUT/runtime" --profile "$OUT/profile-sealed.json" --request examples/requests/in-domain.json --metadata examples/dra/metadata-good.json --signature "$OUT/profile-signature.json" --public-key "$OUT/keys/signing-public.pem" --require-signature --at 2026-08-20T03:00:00Z
mv "$OUT/runtime/bin/demo-kernel-tampered" "$OUT/runtime/bin/demo-kernel"; chmod 0500 "$OUT/runtime/bin/demo-kernel"

expect_fail signature-required 'CONTRACT_SIGNATURE_REQUIRED' "$OUT/c2k" launch --verify-only --root "$OUT/runtime" --profile "$OUT/profile-sealed.json" --request examples/requests/in-domain.json --metadata examples/dra/metadata-good.json --require-signature --at 2026-08-20T03:00:00Z
"$OUT/c2k" sign --profile "$OUT/profile-sealed.json" --private-key "$OUT/keys/signing-private.pem" --out "$OUT/future-signature.json" --at 2026-12-01T00:00:00Z
expect_fail future-signature 'future beyond allowed skew' "$OUT/c2k" verify-signature --profile "$OUT/profile-sealed.json" --signature "$OUT/future-signature.json" --public-key "$OUT/keys/signing-public.pem" --at 2026-08-20T03:00:00Z

"$OUT/c2k" statecheck --trace examples/traces/valid.json >/dev/null && pass valid-state-trace
for pair in \
 'uncertified|examples/traces/no-uncertified-dispatch.json|NoUncertifiedDispatch' \
 'stale|examples/traces/no-stale-profile-reuse.json|NoStaleProfileReuse' \
 'numerical|examples/traces/no-numerical-downgrade.json|NoNumericalDowngrade' \
 'reinterpretation|examples/traces/allocation-non-reinterpretation.json|AllocationNonReinterpretation' \
 'resurrection|examples/traces/no-performance-resurrection.json|NoPerformanceResurrection' \
 'active-republish|examples/traces/active-republish.json|NoStaleProfileReuse' \
 'duplicate-admit|examples/traces/duplicate-admit.json|admitted more than once'; do
  IFS='|' read -r name trace needle <<<"$pair"
  expect_fail "state-${name}" "$needle" "$OUT/c2k" statecheck --trace "$trace"
done

# A raw missing path must be a controlled CLI error, not a stack trace.
expect_fail missing-file 'read does-not-exist.json' "$OUT/c2k" validate --request does-not-exist.json

printf '{"adversarialSuite":"passed","cases":%d,"report":"%s"}\n' "$COUNT" "$REPORT"
