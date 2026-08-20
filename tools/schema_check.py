#!/usr/bin/env python3
"""Validate static Claim2Kernel examples against the Draft 2020-12 schema."""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker

ROOT = Path(__file__).resolve().parents[1]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "paths",
        nargs="*",
        help="optional JSON files; defaults to valid static profile/request examples",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    schema_path = ROOT / "schemas/claim2kernel-v1alpha1.schema.json"
    schema = json.loads(schema_path.read_text())
    Draft202012Validator.check_schema(schema)
    validator = Draft202012Validator(schema, format_checker=FormatChecker())
    if args.paths:
        paths = [Path(p) for p in args.paths]
    else:
        paths = sorted((ROOT / "examples").glob("profile-*.json"))
        paths += sorted((ROOT / "examples/requests").glob("*.json"))
    if not paths:
        raise ValueError("no JSON files selected")
    failures = 0
    for path in paths:
        try:
            value = json.loads(path.read_text())
        except Exception as exc:
            print(f"FAIL {path}: decode: {exc}", file=sys.stderr)
            failures += 1
            continue
        errors = sorted(validator.iter_errors(value), key=lambda e: list(e.absolute_path))
        if errors:
            failures += 1
            for error in errors:
                location = "/".join(str(p) for p in error.absolute_path) or "<root>"
                print(f"FAIL {path}:{location}: {error.message}", file=sys.stderr)
        else:
            print(f"PASS {path}")
    if failures:
        raise SystemExit(f"schema validation failed for {failures} file(s)")
    print(json.dumps({"schema": "passed", "files": len(paths)}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
