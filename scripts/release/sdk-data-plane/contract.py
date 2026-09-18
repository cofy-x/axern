from __future__ import annotations

from collections.abc import Callable, Iterable
from typing import Any, Protocol


class DeclaredOutputLike(Protocol):
    path: str
    format: Any
    media_type: str


def normalized_declared_output_contract(
    outputs: Iterable[DeclaredOutputLike],
    *,
    format_number: Callable[[Any], int],
) -> list[tuple[str, int, str]]:
    """Return the order-independent, multiplicity-preserving output contract."""
    return sorted(
        (output.path, format_number(output.format), output.media_type)
        for output in outputs
    )
