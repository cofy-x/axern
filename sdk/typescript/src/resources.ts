/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { SandboxValidationError } from "./errors/index.js";

export type ResourceQuantity = string | number;

export interface ResourceOptions {
  requestCpu?: ResourceQuantity;
  requestMemory?: ResourceQuantity;
  requestEphemeralStorage?: ResourceQuantity;
  limitCpu?: ResourceQuantity;
  limitMemory?: ResourceQuantity;
  limitEphemeralStorage?: ResourceQuantity;
}

export function buildResourceSpec(options: ResourceOptions): Record<string, unknown> | undefined {
  const requests = resourceQuantity(
    "requestCpu",
    options.requestCpu,
    "requestMemory",
    options.requestMemory,
    "requestEphemeralStorage",
    options.requestEphemeralStorage,
  );
  const limits = resourceQuantity(
    "limitCpu",
    options.limitCpu,
    "limitMemory",
    options.limitMemory,
    "limitEphemeralStorage",
    options.limitEphemeralStorage,
  );
  if (requests === undefined && limits === undefined) {
    return undefined;
  }
  return { requests, limits };
}

function resourceQuantity(
  cpuName: string,
  cpuValue: ResourceQuantity | undefined,
  memoryName: string,
  memoryValue: ResourceQuantity | undefined,
  ephemeralStorageName: string,
  ephemeralStorageValue: ResourceQuantity | undefined,
): Record<string, number> | undefined {
  const cpu = parseCpuQuantity(cpuName, cpuValue);
  const memory = parseMemoryQuantity(memoryName, memoryValue);
  const ephemeralStorage = parseMemoryQuantity(ephemeralStorageName, ephemeralStorageValue);
  if (cpu <= 0 && memory <= 0 && ephemeralStorage <= 0) {
    return undefined;
  }
  return {
    cpu_milli: cpu,
    memory_bytes: memory,
    ephemeral_storage_bytes: ephemeralStorage,
  };
}

function parseCpuQuantity(name: string, value: ResourceQuantity | undefined): number {
  const quantity = resourceQuantityText(name, value);
  if (quantity === "") {
    return 0;
  }
  if (quantity.startsWith("-")) {
    throw new SandboxValidationError(`${name} must be non-negative`);
  }
  if (quantity.endsWith("m")) {
    return parseScaledDecimal(name, quantity.slice(0, -1), 1n);
  }
  return parseScaledDecimal(name, quantity, 1000n);
}

function parseMemoryQuantity(name: string, value: ResourceQuantity | undefined): number {
  const quantity = resourceQuantityText(name, value);
  if (quantity === "") {
    return 0;
  }
  if (quantity.startsWith("-")) {
    throw new SandboxValidationError(`${name} must be non-negative`);
  }
  const [number, factor] = splitMemoryQuantity(quantity);
  return parseScaledDecimal(name, number, factor);
}

function resourceQuantityText(name: string, value: ResourceQuantity | undefined): string {
  if (value === undefined) {
    return "";
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      throw new SandboxValidationError(`${name} must be a resource quantity`);
    }
    return String(value);
  }
  return value.trim();
}

function splitMemoryQuantity(value: string): [string, bigint] {
  const units: Array<[string, bigint]> = [
    ["tib", 1024n ** 4n],
    ["gib", 1024n ** 3n],
    ["mib", 1024n ** 2n],
    ["kib", 1024n],
    ["ti", 1024n ** 4n],
    ["gi", 1024n ** 3n],
    ["mi", 1024n ** 2n],
    ["ki", 1024n],
    ["tb", 1000n ** 4n],
    ["gb", 1000n ** 3n],
    ["mb", 1000n ** 2n],
    ["kb", 1000n],
    ["b", 1n],
  ];
  const lower = value.toLowerCase();
  for (const [suffix, factor] of units) {
    if (lower.endsWith(suffix)) {
      return [value.slice(0, -suffix.length).trim(), factor];
    }
  }
  return [value, 1n];
}

function parseScaledDecimal(name: string, value: string, scale: bigint): number {
  const quantity = value.trim();
  if (quantity === "") {
    throw new SandboxValidationError(`${name} is required`);
  }
  const match = quantity.match(/^(\+)?(\d+|\d*\.\d+)$/);
  if (match === null) {
    throw new SandboxValidationError(`${name} must be a decimal number`);
  }
  const [wholePart, fractionPart = ""] = quantity.replace(/^\+/, "").split(".");
  const whole = wholePart === "" ? "0" : wholePart;
  const numerator = BigInt(`${whole}${fractionPart}`) * scale;
  const denominator = 10n ** BigInt(fractionPart.length);
  if (numerator % denominator !== 0n) {
    throw new SandboxValidationError(`${name} must resolve to a whole unit`);
  }
  const parsed = numerator / denominator;
  if (parsed > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new SandboxValidationError(`${name} is too large`);
  }
  return Number(parsed);
}
