#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 1 ]]; then
  echo "usage: $0 <release.zip>" >&2
  exit 2
fi
ZIP=$(realpath "$1")
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
unzip -q "$ZIP" -d "$TMP"
mapfile -t roots < <(find "$TMP" -mindepth 1 -maxdepth 1 -type d)
if [[ ${#roots[@]} -ne 1 ]]; then
  echo "archive must contain exactly one top-level directory" >&2
  exit 1
fi
cd "${roots[0]}"
sha256sum -c MANIFEST.sha256
# Private signing material and generated measurements must never ship.
if find . -type f \( -name '*private*.pem' -o -path './artifacts/*' ! -name '.gitkeep' \) | grep -q .; then
  echo "release contains private keys or generated artifacts" >&2
  exit 1
fi
if grep -RIl --exclude='MANIFEST.sha256' -E 'BEGIN (OPENSSH |EC |RSA )?PRIVATE KEY' . >/dev/null; then
  echo "release contains private-key material" >&2
  exit 1
fi
printf '{"packageVerification":"passed","root":"%s"}\n' "$(basename "${roots[0]}")"
