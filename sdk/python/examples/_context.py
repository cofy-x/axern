"""Shared explicit context loading for Python SDK examples."""

from __future__ import annotations

import os
from pathlib import Path

from axern_sdk import AxernContext, load_context


def current_context() -> AxernContext:
    path = os.environ.get("AXERN_CONFIG", str(Path.home() / ".config" / "axern" / "config.json"))
    return load_context(path, os.environ.get("AXERN_CONTEXT", ""))
