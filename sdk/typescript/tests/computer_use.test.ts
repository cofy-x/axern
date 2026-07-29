/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { capabilityStatus } from "../src/node/capabilities.js";
import {
  computerUseDisplay,
  computerUseKeyboard,
  computerUseMouse,
  computerUseScreenshot,
  computerUseStatus,
} from "../src/node/computer_use.js";
import type { NodeClientContext } from "../src/node/context.js";

test("capability status preserves provider detail", async () => {
  const { ctx, requests } = fakeContext({
    CapabilityStatus: {
      ready: true,
      capabilities: ["computer-use"],
      providers: [{
        name: "desktop",
        state: "ready",
        available: true,
        capabilities: ["computer-use"],
        backend: "runsc",
        reason: "",
        dependencies: [{ name: "display", available: true, reason: "" }],
      }],
      provider_summary: { total: 1, available: 1, degraded: 0, unavailable: 0 },
    },
  });

  const status = await capabilityStatus(ctx);

  assert.equal(status.ready, true);
  assert.equal(status.providers[0]?.backend, "runsc");
  assert.deepEqual(status.providerSummary, { total: 1, available: 1, degraded: 0, unavailable: 0 });
  assert.deepEqual(requests.CapabilityStatus, [{ allocation_id: "alloc-1" }]);
});

test("computer use requests and responses preserve the public contract", async () => {
  const { ctx, requests } = fakeContext({
    ComputerUseStatus: {
      available: true,
      display: ":0",
      backend: "x11",
      dependencies: [{ name: "display", available: true }],
    },
    ComputerUseScreenshot: { data: Buffer.from("png"), content_type: "image/png" },
    ComputerUseDisplay: { display: ":0", backend: "x11", width: 1280, height: 720 },
    ComputerUseMouse: {},
    ComputerUseKeyboard: {},
  });

  const status = await computerUseStatus(ctx);
  const screenshot = await computerUseScreenshot(ctx, {
    showCursor: true,
    region: { x: 1, y: 2, width: 3, height: 4 },
    format: "png",
    quality: 90,
    scale: 1,
  });
  const display = await computerUseDisplay(ctx);
  await computerUseMouse(ctx, { action: "click", x: 10, y: 20, button: "left" });
  await computerUseKeyboard(ctx, { text: "hello", keys: ["ENTER"], delayMs: 5 });

  assert.equal(status.available, true);
  assert.equal(status.dependencies[0]?.name, "display");
  assert.equal(screenshot.data.toString(), "png");
  assert.equal(screenshot.contentType, "image/png");
  assert.deepEqual(display, { display: ":0", backend: "x11", width: 1280, height: 720 });
  assert.deepEqual(requests.ComputerUseScreenshot?.[0]?.region, { x: 1, y: 2, width: 3, height: 4 });
  assert.equal(requests.ComputerUseMouse?.[0]?.action, "click");
  assert.deepEqual(requests.ComputerUseKeyboard?.[0]?.keys, ["ENTER"]);
  for (const entries of Object.values(requests)) {
    assert.equal(entries[0]?.allocation_id, "alloc-1");
  }
});

function fakeContext(responses: Record<string, Record<string, unknown>>): {
  ctx: NodeClientContext;
  requests: Record<string, Record<string, unknown>[]>;
} {
  const requests: Record<string, Record<string, unknown>[]> = {};
  const client: Record<string, unknown> = {};
  for (const [method, response] of Object.entries(responses)) {
    client[method] = (
      request: Record<string, unknown>,
      callback: (error: Error | null, value: Record<string, unknown>) => void,
    ) => {
      (requests[method] ??= []).push(request);
      callback(null, response);
    };
  }
  client.close = () => undefined;
  const ctx = {
    allocationId: "alloc-1",
    authRequest(payload: Record<string, unknown>) {
      return { allocation_id: "alloc-1", ...payload };
    },
    async withAuthRetry(_ttl: number, operation: (value: unknown) => Promise<unknown>) {
      return operation(client);
    },
  } as unknown as NodeClientContext;
  return { ctx, requests };
}
