#!/usr/bin/env python3
"""Reconcile an immutable Axern formula into the official Homebrew tap."""

from __future__ import annotations

import os
import pathlib
import re
import sys
import tempfile


VERSION_PATTERN = re.compile(r'^\s*version "([0-9]+)\.([0-9]+)\.([0-9]+)"\s*$', re.MULTILINE)


class FormulaError(RuntimeError):
    pass


def formula_version(content: bytes, label: str) -> tuple[int, int, int]:
    try:
        text = content.decode("utf-8")
    except UnicodeDecodeError as error:
        raise FormulaError(f"{label} is not UTF-8") from error
    match = VERSION_PATTERN.search(text)
    if not match:
        raise FormulaError(f"{label} does not declare a stable semantic version")
    return int(match.group(1)), int(match.group(2)), int(match.group(3))


def atomic_write(path: pathlib.Path, content: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary_path: pathlib.Path | None = None
    try:
        with tempfile.NamedTemporaryFile(dir=path.parent, prefix=f".{path.name}.", delete=False) as handle:
            handle.write(content)
            temporary_path = pathlib.Path(handle.name)
        temporary_path.chmod(0o644)
        os.replace(temporary_path, path)
        temporary_path = None
    finally:
        if temporary_path is not None:
            temporary_path.unlink(missing_ok=True)


def reconcile(candidate: pathlib.Path, target: pathlib.Path) -> bool:
    candidate_content = candidate.read_bytes()
    candidate_version = formula_version(candidate_content, str(candidate))
    if not target.exists():
        atomic_write(target, candidate_content)
        return True

    target_content = target.read_bytes()
    target_version = formula_version(target_content, str(target))
    if target_version > candidate_version:
        raise FormulaError(
            f"refusing Homebrew formula downgrade from {target_version} to {candidate_version}"
        )
    if target_version == candidate_version:
        if target_content != candidate_content:
            raise FormulaError(
                f"Homebrew formula version {candidate_version} already exists with different content"
            )
        return False

    atomic_write(target, candidate_content)
    return True


def output(name: str, value: str) -> None:
    print(f"{name}={value}")
    github_output = os.environ.get("GITHUB_OUTPUT")
    if github_output:
        with pathlib.Path(github_output).open("a", encoding="utf-8") as handle:
            handle.write(f"{name}={value}\n")


def main() -> int:
    if len(sys.argv) != 3:
        print(f"usage: {sys.argv[0]} CANDIDATE TARGET", file=sys.stderr)
        return 2
    candidate = pathlib.Path(sys.argv[1])
    target = pathlib.Path(sys.argv[2])
    try:
        changed = reconcile(candidate, target)
    except (FormulaError, OSError) as error:
        print(f"Homebrew formula reconciliation failed: {error}", file=sys.stderr)
        return 1
    output("homebrew_formula_changed", str(changed).lower())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
