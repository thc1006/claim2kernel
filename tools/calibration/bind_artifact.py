#!/usr/bin/env python3
"""Bind exact artifact bytes into an otherwise unsealed KernelProfile."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path

from jsonsafe import load_json


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", required=True)
    parser.add_argument("--artifact", required=True)
    parser.add_argument("--root", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--source", help="source file whose SHA-256 is recorded")
    parser.add_argument("--dataset", help="dataset file whose SHA-256 is recorded")
    parser.add_argument("--compiler", help="exact compiler/toolchain identity")
    parser.add_argument("--compiler-file", help="compiler binary/package whose SHA-256 is recorded")
    parser.add_argument("--git-commit", help="optional immutable source revision")
    args = parser.parse_args()

    profile = load_json(args.profile)
    if profile.get("seal") is not None:
        raise ValueError("refusing to rebind a sealed profile")
    root = Path(args.root).resolve(strict=True)
    artifact = Path(args.artifact).resolve(strict=True)
    try:
        relative = artifact.relative_to(root)
    except ValueError as exc:
        raise ValueError("artifact must resolve beneath --root") from exc
    stat = artifact.stat()
    if not artifact.is_file() or os.path.islink(args.artifact):
        raise ValueError("artifact must be a non-symlink regular file")
    if stat.st_mode & 0o022:
        raise ValueError("artifact must not be group- or world-writable")
    if stat.st_mode & 0o111 == 0:
        raise ValueError("artifact must be executable")

    spec = profile["spec"]["artifact"]
    max_bytes = int(spec.get("maxBytes", 0))
    if max_bytes <= 0 or max_bytes > 2 << 30:
        raise ValueError("declared maxBytes must be in [1,2GiB]")
    if stat.st_size <= 0 or stat.st_size > max_bytes:
        raise ValueError(f"artifact size {stat.st_size} exceeds declared maxBytes {max_bytes}")
    digest = hashlib.sha256()
    size = 0
    with artifact.open("rb") as handle:
        while True:
            chunk = handle.read(min(1024 * 1024, max_bytes + 1 - size))
            if not chunk:
                break
            digest.update(chunk)
            size += len(chunk)
            if size > max_bytes:
                raise ValueError(f"artifact changed while hashing and exceeded maxBytes {max_bytes}")
    if size != stat.st_size:
        raise ValueError(f"artifact changed while hashing: stat={stat.st_size} read={size}")
    spec["path"] = relative.as_posix()
    spec["digest"] = "sha256:" + digest.hexdigest()
    spec["sizeBytes"] = size
    provenance = profile["spec"]["provenance"]
    if args.source:
        provenance["sourceDigest"] = digest_file(Path(args.source).resolve(strict=True))
    if args.dataset:
        provenance["datasetDigest"] = digest_file(Path(args.dataset).resolve(strict=True))
    if args.compiler:
        provenance["compiler"] = args.compiler
    if args.compiler_file:
        provenance["compilerDigest"] = digest_file(Path(args.compiler_file).resolve(strict=True))
    if args.git_commit:
        provenance["gitCommit"] = args.git_commit

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    tmp = out.with_name("." + out.name + ".tmp")
    tmp.write_text(json.dumps(profile, indent=2, ensure_ascii=False, allow_nan=False) + "\n")
    tmp.replace(out)


def digest_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


if __name__ == "__main__":
    main()
