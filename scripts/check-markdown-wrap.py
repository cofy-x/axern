#!/usr/bin/env python3
"""Reject column-based line wrapping inside Markdown prose."""

from __future__ import annotations

import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


FENCE_RE = re.compile(r"^\s*(```|~~~)")
HEADING_RE = re.compile(r"^\s{0,3}#{1,6}(?:\s|$)")
LIST_RE = re.compile(r"^(\s*)(?:[-+*]|\d+[.)])\s+(.*)$")
QUOTE_RE = re.compile(r"^(\s*(?:>\s*)+)(.*)$")
TABLE_SEPARATOR_RE = re.compile(
    r"^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?\s*$"
)
THEMATIC_BREAK_RE = re.compile(r"^\s{0,3}(?:[-*_]\s*){3,}$")
LINK_DEFINITION_RE = re.compile(r"^\s*\[[^]]+\]:\s*\S+")


@dataclass
class TextLine:
    number: int
    raw: str
    quote_prefix: str
    in_list: bool


def repository_markdown() -> list[Path]:
    result = subprocess.run(
        [
            "git",
            "ls-files",
            "--cached",
            "--others",
            "--exclude-standard",
            "--",
            "*.md",
            "*.mdx",
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    return [Path(name) for name in result.stdout.splitlines() if Path(name).is_file()]


def has_explicit_break(line: str) -> bool:
    stripped = line.rstrip()
    return line.endswith("  ") or stripped.endswith(("<br>", "<br/>", "<br />", "\\"))


def structural(body: str, previous: str, following: str) -> bool:
    stripped = body.strip()
    if not stripped:
        return True
    if HEADING_RE.match(body) or THEMATIC_BREAK_RE.match(body):
        return True
    if TABLE_SEPARATOR_RE.match(body) or LINK_DEFINITION_RE.match(body):
        return True
    if stripped.startswith(("|", "![", "<!--", "-->", ":::", "$$")):
        return True
    if stripped.startswith(("<", "</", "{", "}")):
        return True
    if "|" in body and (
        TABLE_SEPARATOR_RE.match(previous) or TABLE_SEPARATOR_RE.match(following)
    ):
        return True
    return False


def check_file(path: Path) -> list[tuple[int, str]]:
    lines = path.read_text(encoding="utf-8").splitlines()
    findings: list[tuple[int, str]] = []
    previous_text: TextLine | None = None
    in_fence = False
    in_frontmatter = False
    in_html_comment = False
    list_context = False

    for index, raw in enumerate(lines):
        number = index + 1
        stripped = raw.strip()

        if number == 1 and stripped == "---":
            in_frontmatter = True
            previous_text = None
            continue
        if in_frontmatter:
            if stripped in {"---", "..."}:
                in_frontmatter = False
            previous_text = None
            continue

        if in_html_comment:
            if "-->" in raw:
                in_html_comment = False
            previous_text = None
            continue
        if "<!--" in raw and "-->" not in raw:
            in_html_comment = True
            previous_text = None
            continue

        if FENCE_RE.match(raw):
            in_fence = not in_fence
            previous_text = None
            continue
        if in_fence:
            continue

        quote = QUOTE_RE.match(raw)
        quote_prefix = quote.group(1) if quote else ""
        body = quote.group(2) if quote else raw
        previous_raw = lines[index - 1] if index else ""
        following_raw = lines[index + 1] if index + 1 < len(lines) else ""

        if structural(body, previous_raw, following_raw):
            if not stripped:
                list_context = False
            previous_text = None
            continue

        list_item = LIST_RE.match(body)
        starts_list_item = list_item is not None
        if list_item:
            text = list_item.group(2).strip()
            current_in_list = True
        else:
            text = body.strip()
            indentation = len(body) - len(body.lstrip(" "))
            if indentation >= 4 and not list_context:
                previous_text = None
                continue
            current_in_list = list_context

        if (
            previous_text is not None
            and text
            and not starts_list_item
            and previous_text.quote_prefix == quote_prefix
            and not has_explicit_break(previous_text.raw)
        ):
            kind = "list item" if previous_text.in_list or current_in_list else "paragraph"
            findings.append(
                (previous_text.number, f"Markdown {kind} is hard-wrapped; join it with line {number}")
            )

        previous_text = TextLine(number, raw, quote_prefix, current_in_list)
        if starts_list_item:
            list_context = True

    return findings


def main() -> int:
    paths = [Path(argument) for argument in sys.argv[1:]] or repository_markdown()
    status = 0
    for path in paths:
        if not path.is_file() or path.suffix.lower() not in {".md", ".mdx"}:
            continue
        for line, message in check_file(path):
            print(f"{path}:{line}: {message}", file=sys.stderr)
            status = 1
    return status


if __name__ == "__main__":
    raise SystemExit(main())
