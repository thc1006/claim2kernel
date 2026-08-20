#!/usr/bin/env python3
"""Bind the RZF *source* and *dataset* digests into the profile, honestly.

The full seal (`c2k seal`) binds a built artifact, container, and compiler. In an
environment without the Mojo compiler or a target GPU, those cannot be produced,
so this tool binds ONLY what actually exists -- the kernel source digest and the
correctness-dataset digest -- and records every other provenance field as an
explicit NOT RUN. It leaves the profile UNSEALED, preserving the fail-closed
invariant: an unsealed profile with a placeholder artifact digest cannot be
admitted at runtime. It refuses to touch an already-sealed profile.

This never fabricates an artifact/container/compiler digest to make a profile
look complete.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from pathlib import Path

from jsonsafe import load_json

PLACEHOLDER_DIGEST = "sha256:" + "0" * 64


def digest_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b""):
            h.update(chunk)
    return "sha256:" + h.hexdigest()


def git_commit(root: Path) -> str | None:
    try:
        out = subprocess.run(["git", "-C", str(root), "rev-parse", "HEAD"],
                             capture_output=True, text=True, check=True)
        return out.stdout.strip() or None
    except Exception:
        return None


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--profile", required=True, help="unsealed KernelProfile template")
    p.add_argument("--source", required=True, help="kernel source whose SHA-256 is the sourceDigest")
    p.add_argument("--dataset", required=True, help="correctness dataset whose SHA-256 is the datasetDigest")
    p.add_argument("--out", required=True)
    p.add_argument("--cuda-baseline", help="optional CUDA baseline .so to record (as a NOTE, not the artifact)")
    p.add_argument("--compiler", help="exact compiler identity if the kernel was actually compiled "
                                      "(e.g. 'Mojo 1.0.0 / MAX 26.5'); omit to record compiler as NOT RUN")
    args = p.parse_args()

    profile = load_json(args.profile)
    if profile.get("seal") is not None:
        raise SystemExit("refusing to rebind a sealed profile")

    source = Path(args.source).resolve(strict=True)
    dataset = Path(args.dataset).resolve(strict=True)
    prov = profile["spec"]["provenance"]
    prov["sourceDigest"] = digest_file(source)
    prov["datasetDigest"] = digest_file(dataset)
    if args.compiler:
        prov["compiler"] = args.compiler
    else:
        prov["compiler"] = ("NOT RUN: Mojo compiler unavailable in this environment; "
                            "requires Mojo 1.0.0b2 / Modular 26.4 (scripts/check_mojo_version.py)")
    prov.pop("compilerDigest", None)  # binary digest still unbound (no artifact seal)
    commit = git_commit(Path.cwd())
    if commit:
        prov["gitCommit"] = commit

    baseline_note = ""
    if args.cuda_baseline:
        so = Path(args.cuda_baseline).resolve(strict=True)
        baseline_note = (f" CUDA baseline {so.name} digest {digest_file(so)} was compiled "
                         "and run on a GTX 1050 (sm_61) as an algorithm/ABI check ONLY; it "
                         "is NOT this profile's artifact and NOT a target-arch result.")

    compiled_note = ""
    if args.compiler:
        compiled_note = (f" The kernel source COMPILES for the declared targets with {args.compiler} "
                         "(build logs under artifacts/mojo/); GPU EXECUTION on sm_80/sm_90/gfx942 is "
                         "still NOT RUN (no such device here).")
    prov["notes"] = (
        "PARTIALLY BOUND. sourceDigest and datasetDigest are real file hashes. "
        "artifact.digest (placeholder), containerDigest, and compilerDigest are NOT RUN: no "
        "deployable Mojo artifact, container, or runtime-validated target GPU in this "
        "environment. Profile intentionally left UNSEALED and unsignable until the hardware "
        "protocol (docs/MOJO_HARDWARE_PROTOCOL.md) produces those digests." + compiled_note + baseline_note
    )

    # Keep the artifact digest as the explicit placeholder; do not fake it.
    profile["spec"]["artifact"]["digest"] = PLACEHOLDER_DIGEST

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(profile, indent=2, ensure_ascii=False, allow_nan=False) + "\n")
    print(json.dumps({
        "written": str(out), "sealed": False,
        "sourceDigest": prov["sourceDigest"], "datasetDigest": prov["datasetDigest"],
        "artifactDigest": "NOT RUN (placeholder)", "containerDigest": "NOT RUN",
        "compilerDigest": "NOT RUN",
    }, indent=2))


if __name__ == "__main__":
    main()
