"""User-facing resource quantity parsing."""

from __future__ import annotations

import re
from decimal import Decimal, InvalidOperation

_DECIMAL_RE = re.compile(r"^\+?(\d+|\d*\.\d+)$")
ResourceQuantity = str | int | float


def cpu_milli(name: str, value: ResourceQuantity = "") -> int:
    value = _quantity_text(name, value)
    if not value:
        return 0
    if value.startswith("-"):
        raise ValueError(f"{name} must be >= 0")
    if value.endswith("m"):
        return _scaled_decimal(name, value[:-1], 1)
    return _scaled_decimal(name, value, 1000)


def memory_bytes(name: str, value: ResourceQuantity = "") -> int:
    value = _quantity_text(name, value)
    if not value:
        return 0
    if value.startswith("-"):
        raise ValueError(f"{name} must be >= 0")
    number, factor = _split_memory(value)
    return _scaled_decimal(name, number, factor)


def _split_memory(value: str) -> tuple[str, int]:
    units = (
        ("tib", 1024**4),
        ("gib", 1024**3),
        ("mib", 1024**2),
        ("kib", 1024),
        ("ti", 1024**4),
        ("gi", 1024**3),
        ("mi", 1024**2),
        ("ki", 1024),
        ("tb", 1000**4),
        ("gb", 1000**3),
        ("mb", 1000**2),
        ("kb", 1000),
        ("b", 1),
    )
    lower = value.lower()
    for suffix, factor in units:
        if lower.endswith(suffix):
            return value[: -len(suffix)].strip(), factor
    return value, 1


def _quantity_text(name: str, value: ResourceQuantity) -> str:
    if isinstance(value, bool):
        raise ValueError(f"{name} must be a resource quantity")
    if isinstance(value, int | float):
        return str(value)
    return value.strip()


def _scaled_decimal(name: str, value: str, scale: int) -> int:
    value = value.strip()
    if not value:
        raise ValueError(f"{name} is required")
    if not _DECIMAL_RE.match(value):
        raise ValueError(f"{name} must be a decimal number")
    try:
        quantity = Decimal(value) * scale
    except InvalidOperation as exc:
        raise ValueError(f"{name} must be a decimal number") from exc
    if not quantity.is_finite():
        raise ValueError(f"{name} must be a decimal number")
    if quantity < 0:
        raise ValueError(f"{name} must be >= 0")
    if quantity != quantity.to_integral_value():
        raise ValueError(f"{name} must resolve to a whole unit")
    return int(quantity)
