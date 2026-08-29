"""Public sandbox egress policy models."""

from __future__ import annotations

import ipaddress
from dataclasses import dataclass
from typing import Literal

from axern.control.common.v1 import common_pb2

_MAX_RULES = 256
_FORBIDDEN_IPV4 = (
    ipaddress.IPv4Network("0.0.0.0/32"),
    ipaddress.IPv4Network("100.100.100.200/32"),
    ipaddress.IPv4Network("127.0.0.0/8"),
    ipaddress.IPv4Network("169.254.0.0/16"),
    ipaddress.IPv4Network("192.0.0.192/32"),
    ipaddress.IPv4Network("224.0.0.0/4"),
)
_FORBIDDEN_IPV6 = (
    ipaddress.IPv6Network("::/128"),
    ipaddress.IPv6Network("::1/128"),
    ipaddress.IPv6Network("fe80::/10"),
    ipaddress.IPv6Network("ff00::/8"),
)


@dataclass(frozen=True, slots=True)
class PortRange:
    """An inclusive TCP or UDP destination port range."""

    start: int
    end: int | None = None

    def __post_init__(self) -> None:
        end = self.start if self.end is None else self.end
        if not 1 <= self.start <= 65535 or not self.start <= end <= 65535:
            raise ValueError("ports must be an inclusive range within 1..65535")
        object.__setattr__(self, "end", end)


@dataclass(frozen=True, slots=True)
class CIDRRule:
    """An explicit non-HTTP CIDR, protocol, and destination-port grant."""

    cidr: str
    protocol: Literal["tcp", "udp"]
    ports: tuple[PortRange, ...]

    def __post_init__(self) -> None:
        try:
            network = ipaddress.ip_network(self.cidr.strip(), strict=False)
        except ValueError as exc:
            raise ValueError(f"invalid CIDR {self.cidr!r}") from exc
        protected = (
            any(network.subnet_of(forbidden) for forbidden in _FORBIDDEN_IPV4)
            if isinstance(network, ipaddress.IPv4Network)
            else any(network.subnet_of(forbidden) for forbidden in _FORBIDDEN_IPV6)
        )
        if protected:
            raise ValueError(f"CIDR {self.cidr!r} targets a protected address range")
        protocol = self.protocol.lower()
        if protocol not in {"tcp", "udp"}:
            raise ValueError("CIDR protocol must be 'tcp' or 'udp'")
        if not self.ports:
            raise ValueError("CIDR rule requires at least one port range")
        ports = tuple(dict.fromkeys(self.ports))
        object.__setattr__(self, "cidr", network.with_prefixlen)
        object.__setattr__(self, "protocol", protocol)
        object.__setattr__(self, "ports", ports)


class NetworkPolicy:
    """A normalized strict or DNS-only sandbox egress policy."""

    __slots__ = ("_cidr_rules", "_domains", "_mode")

    def __init__(self, mode: Literal["strict", "dns_deny"], domains: tuple[str, ...], cidr_rules: tuple[CIDRRule, ...] = ()) -> None:
        if len(domains) + len(cidr_rules) > _MAX_RULES:
            raise ValueError(f"network policy may contain at most {_MAX_RULES} rules")
        self._mode = mode
        self._domains = domains
        self._cidr_rules = cidr_rules

    @classmethod
    def strict(cls, *domains: str, cidr_rules: tuple[CIDRRule, ...] = ()) -> "NetworkPolicy":
        return cls("strict", _normalize_domains(domains), _dedupe_cidrs(cidr_rules))

    @classmethod
    def allow_domains(cls, *domains: str) -> "NetworkPolicy":
        return cls.strict(*domains)

    @classmethod
    def deny_dns(cls, *domains: str) -> "NetworkPolicy":
        normalized = _normalize_domains(domains)
        if not normalized:
            raise ValueError("deny_dns requires at least one domain")
        return cls("dns_deny", normalized)

    @classmethod
    def deny_all(cls) -> "NetworkPolicy":
        return cls.strict()

    def _to_proto(self) -> common_pb2.NetworkEgressPolicy:
        if self._mode == "dns_deny":
            return common_pb2.NetworkEgressPolicy(dns_deny=common_pb2.DnsDenyPolicy(denied_domains=self._domains))
        return common_pb2.NetworkEgressPolicy(
            strict=common_pb2.StrictEgressPolicy(
                allowed_domains=self._domains,
                allowed_cidrs=[
                    common_pb2.CIDREgressRule(
                        cidr=rule.cidr,
                        protocol=(
                            common_pb2.EGRESS_PROTOCOL_TCP
                            if rule.protocol == "tcp"
                            else common_pb2.EGRESS_PROTOCOL_UDP
                        ),
                        ports=[common_pb2.PortRange(start=port.start, end=port.end) for port in rule.ports],
                    )
                    for rule in self._cidr_rules
                ],
            )
        )


def _normalize_domains(values: tuple[str, ...]) -> tuple[str, ...]:
    result: list[str] = []
    seen: set[str] = set()
    for value in values:
        raw = value.strip().lower()
        wildcard = raw.startswith("*.")
        if wildcard:
            raw = raw[2:]
        raw = raw.rstrip(".")
        if not raw or any(char in raw for char in "/:@?#") or "*" in raw:
            raise ValueError(f"invalid domain rule {value!r}")
        try:
            if ipaddress.ip_address(raw):
                raise ValueError(f"domain rule must not be an IP literal: {value!r}")
        except ValueError as exc:
            if "must not be" in str(exc):
                raise
        try:
            normalized = raw.encode("idna").decode("ascii").lower()
        except UnicodeError as exc:
            raise ValueError(f"invalid domain rule {value!r}") from exc
        labels = normalized.split(".")
        if len(normalized.encode("ascii")) > 253 or any(not label or len(label) > 63 for label in labels):
            raise ValueError(f"invalid domain rule {value!r}")
        normalized = f"*.{normalized}" if wildcard else normalized
        if normalized not in seen:
            seen.add(normalized)
            result.append(normalized)
    return tuple(result)


def _dedupe_cidrs(values: tuple[CIDRRule, ...]) -> tuple[CIDRRule, ...]:
    return tuple(dict.fromkeys(values))
