#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
VERSION=${VERSION:-0.1.0}
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-1787184000}
NAME="claim2kernel-reference-v${VERSION}"
STAGE="$ROOT/dist/$NAME"
ZIP="$ROOT/dist/$NAME.zip"
TMP_MANIFEST="$ROOT/dist/.${NAME}.manifest.tmp"
rm -rf "$STAGE" "$ZIP" "$ZIP.sha256" "$TMP_MANIFEST"
mkdir -p "$STAGE"

# Source-only package: generated private keys, build products, caches, virtual
# environments, and raw local measurements are deliberately excluded.
rsync -a ./ "$STAGE/" \
  --exclude '/dist/' \
  --exclude '/artifacts/*' \
  --exclude '/.venv/' \
  --exclude '/venv/' \
  --exclude '**/__pycache__/' \
  --exclude '*.pyc' \
  --exclude '.git/'
mkdir -p "$STAGE/artifacts"
touch "$STAGE/artifacts/.gitkeep"

# Generate the content manifest outside STAGE so it cannot accidentally hash a
# partially written copy of itself.
(
  cd "$STAGE"
  find . -type f ! -name MANIFEST.sha256 -print0 \
    | LC_ALL=C sort -z \
    | xargs -0 sha256sum > "$TMP_MANIFEST"
)
mv "$TMP_MANIFEST" "$STAGE/MANIFEST.sha256"

# Normalize archive timestamps and omit host-specific extra fields. The source
# ZIP is reproducible when inputs and SOURCE_DATE_EPOCH are identical.
find "$STAGE" -print0 | xargs -0 touch -h -d "@$SOURCE_DATE_EPOCH"
(
  cd "$ROOT/dist"
  find "$NAME" -print | LC_ALL=C sort | zip -X -q "$ZIP" -@
)
(
  cd "$(dirname "$ZIP")"
  sha256sum "$(basename "$ZIP")" > "$(basename "$ZIP").sha256"
)
"$ROOT/scripts/verify_package.sh" "$ZIP" >/dev/null
printf '{"package":"%s","sha256":"%s"}\n' "$ZIP" "$(sha256sum "$ZIP" | awk '{print $1}')"
