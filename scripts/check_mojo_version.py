#!/usr/bin/env python3
"""Validate the Mojo compiler against the repository's tested version window."""
from __future__ import annotations

import re
import sys

text = " ".join(sys.argv[1:])
match = re.search(
    r"(?<![0-9A-Za-z])(?:Mojo\s+)?"
    r"(1\.[0-9]+\.[0-9]+(?:(?:a|b|rc)[0-9]+)?)"
    r"(?![0-9A-Za-z])",
    text,
    re.IGNORECASE,
)
if not match:
    raise SystemExit(f"cannot parse Mojo version from: {text!r}")

raw = match.group(1).lower()
parts = re.fullmatch(r"([0-9]+)\.([0-9]+)\.([0-9]+)(?:(a|b|rc)([0-9]+))?", raw)
assert parts is not None
major, minor, patch = (int(parts.group(i)) for i in range(1, 4))
pre_tag = parts.group(4)
pre_num = int(parts.group(5) or 0)
phase = {"a": 0, "b": 1, "rc": 2, None: 3}[pre_tag]
version_key = (major, minor, patch, phase, pre_num)
minimum = (1, 0, 0, 1, 2)  # 1.0.0b2
maximum = (1, 1, 0, 0, 0)  # every 1.1.0 prerelease/final is outside

if version_key < minimum or version_key >= maximum:
    raise SystemExit(
        f"Mojo {raw} is outside the validated >=1.0.0b2,<1.1.0 range"
    )
print(raw)
