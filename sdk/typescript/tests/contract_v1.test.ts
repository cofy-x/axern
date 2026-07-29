/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { AxernClient } from "../src/client/index.js";
import { loadAxernContext } from "../src/config/index.js";
import {
  AxernRpcError,
  errorRetryable,
  isCancelled,
  isNotFound,
  isPermissionDenied,
  isTimeout,
  isUnavailable,
  SandboxValidationError,
} from "../src/errors/index.js";
import { buildResourceSpec } from "../src/resources.js";
import { validateSandboxOptions } from "../src/sandbox/lifecycle.js";
import { Sandbox } from "../src/sandbox/index.js";
import type { SandboxOptions } from "../src/sandbox/index.js";

interface QuantityCase { input: string; value: number }
interface ErrorCase { code: string; number: number; class: string; retryable: boolean }
interface SourceCase { template?: string; image?: string; environment?: string }
interface ContextCase { name: string; context: Record<string, unknown> }

type TunnelContract = "tunnel" extends keyof SandboxOptions ? true : never;
const tunnelContract: TunnelContract = true;

test("shared resource contract", () => {
  const contract = load<{
    cpu: QuantityCase[];
    memory: QuantityCase[];
    invalid_cpu: string[];
    invalid_memory: string[];
  }>("resources.json");
  for (const item of contract.cpu) {
    const spec = buildResourceSpec({ requestCpu: item.input }) as { requests: { cpu_milli: number } };
    assert.equal(spec.requests.cpu_milli, item.value);
  }
  for (const item of contract.memory) {
    const spec = buildResourceSpec({ requestMemory: item.input }) as { requests: { memory_bytes: number } };
    assert.equal(spec.requests.memory_bytes, item.value);
  }
  for (const value of contract.invalid_cpu) {
    assert.throws(() => buildResourceSpec({ requestCpu: value }), SandboxValidationError);
  }
  for (const value of contract.invalid_memory) {
    assert.throws(() => buildResourceSpec({ requestMemory: value }), SandboxValidationError);
  }
});

test("shared error contract", () => {
  const contract = load<{ rpc: ErrorCase[] }>("errors.json");
  for (const item of contract.rpc) {
    const error = new AxernRpcError("contract", { code: item.number, details: item.code }, { allocationId: "alloc-contract" });
    const matched: Record<string, boolean> = {
      not_found: isNotFound(error),
      permission_denied: isPermissionDenied(error),
      timeout: isTimeout(error),
      cancelled: isCancelled(error),
      unavailable: isUnavailable(error),
    };
    assert.equal(matched[item.class], true, item.code);
    assert.equal(errorRetryable(error), item.retryable, item.code);
    assert.equal(error.operation, "contract");
    assert.equal(error.allocationId, "alloc-contract");
    assert.equal(error.details, item.code);
  }
});

test("shared sandbox source contract", () => {
  const contract = load<{ valid: SourceCase[]; invalid: SourceCase[] }>("sandbox_sources.json");
  const options = (item: SourceCase) => ({
    templateId: item.template,
    image: item.image,
    environmentId: item.environment,
  });
  for (const item of contract.valid) {
    assert.doesNotThrow(() => validateSandboxOptions(options(item)));
  }
  for (const item of contract.invalid) {
    assert.throws(() => validateSandboxOptions(options(item)), SandboxValidationError);
  }
});

test("shared context contract", () => {
  const contract = load<{ valid: ContextCase[]; invalid: ContextCase[] }>("contexts.json");
  for (const item of contract.valid) {
    withContext(item, (path) => {
      const context = loadAxernContext(path);
      assert.equal(context.endpoint, item.context.endpoint);
      assert.equal(context.proxyMode, item.context.proxy_mode);
    });
  }
  for (const item of contract.invalid) {
    withContext(item, (path) => assert.throws(() => loadAxernContext(path)));
  }
});

test("shared common core surface", () => {
  const contract = load<{ client: string[]; sandbox: string[]; agent_sandbox: string[] }>("common_core.json");
  assertMethods(contract.client, AxernClient.prototype, {
    environment_create: "createEnvironment",
    environment_delete: "deleteEnvironment",
    service_create: "createService",
    service_delete: "deleteService",
    service_replicas: "listServiceReplicas",
  });
  assertMethods(contract.sandbox.filter((operation) => operation !== "tunnel"), Sandbox.prototype, {
    lifecycle_start: "start",
    lifecycle_close: "close",
    exec: "exec",
    process: "process",
    file_stat: "stat",
    file_read: "readFile",
    file_write: "writeFile",
    archive_upload: "uploadDir",
    archive_download: "downloadDir",
  });
  assert.equal(tunnelContract, true);
  assertMethods(contract.agent_sandbox, Sandbox.prototype, {
    capability_status: "capabilityStatus",
    computer_use_status: "computerUseStatus",
    computer_use_screenshot: "computerUseScreenshot",
    computer_use_display: "computerUseDisplay",
    computer_use_mouse: "computerUseMouse",
    computer_use_keyboard: "computerUseKeyboard",
  });
});

function load<T>(name: string): T {
  const url = new URL(`../../contracts/v1/${name}`, import.meta.url);
  return JSON.parse(readFileSync(url, "utf8")) as T;
}

function withContext(item: ContextCase, run: (path: string) => void): void {
  const directory = mkdtempSync(join(tmpdir(), "axern-sdk-contract-"));
  const path = join(directory, "config.json");
  writeFileSync(path, JSON.stringify({ current_context: item.name, contexts: { [item.name]: item.context } }));
  try {
    run(path);
  } finally {
    rmSync(directory, { recursive: true });
  }
}

function assertMethods(
  operations: string[],
  target: object,
  mapping: Record<string, string>,
): void {
  for (const operation of operations) {
    const method = mapping[operation];
    assert.notEqual(method, undefined, `shared operation ${JSON.stringify(operation)} has no TypeScript SDK mapping`);
    assert.equal(typeof (target as Record<string, unknown>)[method], "function", `TypeScript SDK method ${method} is missing`);
  }
}
