#!/usr/bin/env python3

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Optional


def fail(message: str) -> None:
    print(f"open-source surface violation: {message}", file=sys.stderr)
    raise SystemExit(1)


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def toml_section(text: str, section: str) -> str:
    match = re.search(
        rf"^\[{re.escape(section)}\]\s*$\n(.*?)(?=^\[|\Z)",
        text,
        flags=re.MULTILINE | re.DOTALL,
    )
    return match.group(1) if match else ""


def require_toml_string(
    root: Path,
    relative: str,
    section: str,
    field: str,
    expected: Optional[str] = None,
) -> str:
    content = toml_section(read_text(root / relative), section)
    match = re.search(rf'^\s*{re.escape(field)}\s*=\s*"([^"]+)"\s*$', content, re.MULTILINE)
    if not match:
        fail(f"{relative} must declare {field} in [{section}]")
    value = match.group(1)
    if expected is not None and value != expected:
        fail(f"{relative} must declare {field} = {expected!r}")
    return value


def main() -> None:
    if len(sys.argv) not in (2, 3):
        raise SystemExit(
            "usage: open-source-public-surface.py <candidate-tree> [private-patterns-file]"
        )
    root = Path(sys.argv[1]).resolve()
    required = (
        "LICENSE",
        "NOTICE",
        "SECURITY.md",
        "CONTRIBUTING.md",
        "CODE_OF_CONDUCT.md",
        "DCO",
        "GOVERNANCE.md",
        "MAINTAINERS.md",
        ".github/CODEOWNERS",
        ".github/PULL_REQUEST_TEMPLATE.md",
        ".github/ISSUE_TEMPLATE/bug_report.yml",
        ".github/ISSUE_TEMPLATE/feature_request.yml",
        ".github/ISSUE_TEMPLATE/config.yml",
        ".github/workflows/dco.yml",
    )
    for relative in required:
        if not (root / relative).is_file():
            fail(f"missing required file {relative}")

    if "Copyright 2026 Chen Yingwei" not in read_text(root / "NOTICE"):
        fail("NOTICE must identify the copyright owner")

    forbidden_patterns = (
        re.compile(r"/Users/[A-Za-z0-9._-]+/"),
        re.compile(r"/home/(?!axern(?:/|$))[A-Za-z0-9._-]+/"),
    )
    private_patterns: list[re.Pattern[str]] = []
    if len(sys.argv) == 3:
        patterns_path = Path(sys.argv[2])
        for line_number, line in enumerate(read_text(patterns_path).splitlines(), start=1):
            expression = line.strip()
            if not expression or expression.startswith("#"):
                continue
            try:
                private_patterns.append(re.compile(expression, re.IGNORECASE))
            except re.error as error:
                fail(f"invalid private pattern at line {line_number}: {error}")

    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        relative = path.relative_to(root).as_posix()
        try:
            text = read_text(path)
        except (UnicodeDecodeError, OSError):
            continue
        for pattern in (*forbidden_patterns, *private_patterns):
            if pattern.search(text):
                fail(f"{relative} matches private pattern {pattern.pattern!r}")

    provider_pattern = re.compile(r"aliyun|alibaba|\bacr\b|\baws\b|\bgcp\b|azure", re.IGNORECASE)
    release_pattern = re.compile(r"release|publish", re.IGNORECASE)
    for workflow in sorted((root / ".github/workflows").glob("*.yml")):
        workflow_text = read_text(workflow)
        if release_pattern.search(workflow.name + workflow_text) and provider_pattern.search(
            workflow.name + workflow_text
        ):
            fail(f"{workflow.relative_to(root)} contains provider-specific release automation")

    uses_pattern = re.compile(r"^\s*uses:\s+[^\s#]+@([^\s#]+)", re.MULTILINE)
    sha_pattern = re.compile(r"^[0-9a-f]{40}$")
    for workflow in sorted((root / ".github/workflows").glob("*.yml")):
        for reference in uses_pattern.findall(read_text(workflow)):
            if not sha_pattern.fullmatch(reference):
                fail(f"{workflow.relative_to(root)} uses an unpinned action ref {reference!r}")

    root_package = json.loads(read_text(root / "package.json"))
    docs_package = json.loads(read_text(root / "apps/docs/package.json"))
    ts_package = json.loads(read_text(root / "sdk/typescript/package.json"))
    for name, package in (
        ("package.json", root_package),
        ("apps/docs/package.json", docs_package),
        ("sdk/typescript/package.json", ts_package),
    ):
        if package.get("license") != "Apache-2.0":
            fail(f"{name} must declare Apache-2.0")
        if not package.get("description"):
            fail(f"{name} must declare description")
        if not package.get("repository") or not package.get("homepage"):
            fail(f"{name} must declare repository and homepage")
    if docs_package.get("private") is not True:
        fail("the documentation package must remain private")
    if ts_package.get("private") is not True:
        fail("the TypeScript SDK must remain private until npm publishing is explicitly enabled")

    for relative in ("pyproject.toml", "sdk/python/pyproject.toml"):
        require_toml_string(root, relative, "project", "description")
        require_toml_string(root, relative, "project", "license", "Apache-2.0")
        require_toml_string(root, relative, "project.urls", "Homepage")
        require_toml_string(root, relative, "project.urls", "Repository")
    require_toml_string(root, "runtime/imagefsd/Cargo.toml", "package", "description")
    require_toml_string(root, "runtime/imagefsd/Cargo.toml", "package", "license", "Apache-2.0")
    require_toml_string(root, "runtime/imagefsd/Cargo.toml", "package", "repository")
    require_toml_string(root, "runtime/imagefsd/Cargo.toml", "package", "homepage")

    print("open_source_public_surface=passed")


if __name__ == "__main__":
    main()
