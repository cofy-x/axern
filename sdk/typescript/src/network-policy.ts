/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { isIP } from "node:net";
import { domainToASCII } from "node:url";

export interface PortRange {
  readonly start: number;
  readonly end: number;
}

export interface CIDRRule {
  readonly cidr: string;
  readonly protocol: "tcp" | "udp";
  readonly ports: readonly PortRange[];
}

export interface StrictNetworkPolicyOptions {
  readonly domains?: readonly string[];
  readonly cidrRules?: readonly CIDRRule[];
}

type NetworkPolicyWire =
  | { strict: { allowed_domains: string[]; allowed_cidrs: Record<string, unknown>[] } }
  | { dns_deny: { denied_domains: string[] } };

const maxRules = 256;

export class NetworkPolicy {
  private constructor(private readonly wire: NetworkPolicyWire) {}

  static strict(options: StrictNetworkPolicyOptions = {}): NetworkPolicy {
    const domains = normalizeDomains(options.domains ?? []);
    const cidrRules = normalizeCIDRRules(options.cidrRules ?? []);
    if (domains.length + cidrRules.length > maxRules) {
      throw new RangeError(`network policy may contain at most ${maxRules} rules`);
    }
    return new NetworkPolicy({
      strict: {
        allowed_domains: domains,
        allowed_cidrs: cidrRules.map((rule) => ({
          cidr: rule.cidr,
          protocol: rule.protocol === "tcp" ? 1 : 2,
          ports: rule.ports.map((port) => ({ start: port.start, end: port.end })),
        })),
      },
    });
  }

  static allowDomains(...domains: string[]): NetworkPolicy {
    return NetworkPolicy.strict({ domains });
  }

  static denyDns(...domains: string[]): NetworkPolicy {
    const normalized = normalizeDomains(domains);
    if (normalized.length === 0) throw new RangeError("denyDns requires at least one domain");
    return new NetworkPolicy({ dns_deny: { denied_domains: normalized } });
  }

  static denyAll(): NetworkPolicy {
    return NetworkPolicy.strict();
  }

  /** @internal */
  toWire(): NetworkPolicyWire {
    return structuredClone(this.wire);
  }
}

export function portRange(start: number, end: number = start): PortRange {
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < 1 || end < start || end > 65535) {
    throw new RangeError("ports must be an inclusive range within 1..65535");
  }
  return Object.freeze({ start, end });
}

export function cidrRule(cidr: string, protocol: "tcp" | "udp", ...ports: PortRange[]): CIDRRule {
  return normalizeCIDRRule({ cidr, protocol, ports });
}

function normalizeDomains(values: readonly string[]): string[] {
  const result: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    let raw = value.trim().toLowerCase();
    const wildcard = raw.startsWith("*.");
    if (wildcard) raw = raw.slice(2);
    raw = raw.replace(/\.+$/, "");
    if (raw === "" || /[/:@?#*]/u.test(raw) || isIP(raw) !== 0) {
      throw new TypeError(`invalid domain rule ${JSON.stringify(value)}`);
    }
    const ascii = domainToASCII(raw).toLowerCase();
    const labels = ascii.split(".");
    if (ascii === "" || Buffer.byteLength(ascii, "ascii") > 253 || labels.some((label) => label === "" || label.length > 63)) {
      throw new TypeError(`invalid domain rule ${JSON.stringify(value)}`);
    }
    const normalized = wildcard ? `*.${ascii}` : ascii;
    if (!seen.has(normalized)) {
      seen.add(normalized);
      result.push(normalized);
    }
  }
  return result;
}

function normalizeCIDRRules(values: readonly CIDRRule[]): CIDRRule[] {
  const result: CIDRRule[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const rule = normalizeCIDRRule(value);
    const key = JSON.stringify(rule);
    if (!seen.has(key)) {
      seen.add(key);
      result.push(rule);
    }
  }
  return result;
}

function normalizeCIDRRule(value: CIDRRule): CIDRRule {
  const cidr = value.cidr.trim();
  const separator = cidr.lastIndexOf("/");
  const address = separator < 0 ? "" : cidr.slice(0, separator);
  const prefix = separator < 0 ? -1 : Number(cidr.slice(separator + 1));
  const family = isIP(address);
  if ((family !== 4 && family !== 6) || !Number.isInteger(prefix) || prefix < 0 || prefix > (family === 4 ? 32 : 128)) {
    throw new TypeError(`invalid CIDR ${JSON.stringify(value.cidr)}`);
  }
  if (isProtectedCIDR(address, prefix, family)) {
    throw new TypeError(`CIDR ${JSON.stringify(value.cidr)} targets a protected address range`);
  }
  if (value.protocol !== "tcp" && value.protocol !== "udp") throw new TypeError("CIDR protocol must be tcp or udp");
  if (value.ports.length === 0) throw new RangeError("CIDR rule requires at least one port range");
  const ports = value.ports.map((port) => portRange(port.start, port.end));
  return Object.freeze({ cidr, protocol: value.protocol, ports: Object.freeze(ports) });
}

function isProtectedCIDR(address: string, prefix: number, family: number): boolean {
  if (family === 4) {
    const value = address.split(".").reduce((result, octet) => (result << 8n) | BigInt(octet), 0n);
    const protectedRanges: readonly [bigint, number][] = [
      [0n, 32],
      [1684301000n, 32], // 100.100.100.200
      [2130706432n, 8], // 127.0.0.0
      [2851995648n, 16], // 169.254.0.0
      [3221225664n, 32], // 192.0.0.192
      [3758096384n, 4], // 224.0.0.0
    ];
    return protectedRanges.some(([base, bits]) => prefix >= bits && (value >> BigInt(32 - bits)) === (base >> BigInt(32 - bits)));
  }
  const canonical = new URL(`http://[${address}]/`).hostname.slice(1, -1).toLowerCase();
  if (prefix >= 128 && (canonical === "::" || canonical === "::1")) return true;
  const first = Number.parseInt(canonical.split(":", 1)[0] || "0", 16);
  return (prefix >= 10 && first >= 0xfe80 && first <= 0xfebf) || (prefix >= 8 && first >= 0xff00 && first <= 0xffff);
}
