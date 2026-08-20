"""Strict, size-bounded JSON helpers for offline certificate tooling."""
from __future__ import annotations

import json
from pathlib import Path
from typing import Any

MAX_JSON_BYTES = 4 << 20


def _object_no_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key!r}")
        result[key] = value
    return result


def load_json(path: str | Path, max_bytes: int = MAX_JSON_BYTES) -> Any:
    source = Path(path)
    data = source.read_bytes()
    if not data:
        raise ValueError(f"{source}: empty JSON file")
    if len(data) > max_bytes:
        raise ValueError(f"{source}: JSON exceeds {max_bytes} bytes")
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError(f"{source}: JSON is not UTF-8") from exc

    def reject_constant(value: str) -> None:
        raise ValueError(f"non-standard JSON number {value!r} is forbidden")

    return json.loads(
        text,
        object_pairs_hook=_object_no_duplicates,
        parse_constant=reject_constant,
    )
