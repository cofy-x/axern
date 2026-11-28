/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";
import type { TestContext } from "node:test";

import { Sandbox } from "../src/index.js";
import type { AxernClient } from "../src/index.js";
import { SandboxValidationError } from "../src/errors/index.js";
import { TunnelConnector } from "../src/tunnel/connector.js";
import { isTerminalRelayError } from "../src/tunnel/relay.js";
import { TunnelRuntime } from "../src/tunnel/runtime.js";

test("tunnel runtime starts connector, waits for client event, renews, and revokes", async () => {
  const calls: string[] = [];
  const revokeStarted = Promise.withResolvers<void>();
  const revokeComplete = Promise.withResolvers<void>();
  const control = {
    async createSession(options: Record<string, unknown>) {
      calls.push(`create:${String(options.allocationId)}:${String(options.proxyPort)}`);
      return {
        session: {
          session_id: "tun-1",
          client_edge_target: "127.0.0.1:24101",
          bound_addr: "127.0.0.1:8786",
        },
        clientToken: "client-token",
      };
    },
    async listEvents(sessionId: string) {
      calls.push(`events:${sessionId}`);
      return [{ event_type: 6 }];
    },
    async getSession(sessionId: string) {
      calls.push(`get:${sessionId}`);
      return {
        session_id: sessionId,
        bound_addr: "127.0.0.1:8786",
      };
    },
    async renewSession(sessionId: string, clientToken: string) {
      calls.push(`renew:${sessionId}:${clientToken}`);
      return { session_id: sessionId };
    },
    async revokeSession(sessionId: string) {
      calls.push(`revoke:${sessionId}`);
      revokeStarted.resolve();
      await revokeComplete.promise;
    },
  };

  const runtime = new TunnelRuntime({
    control,
    allocationId: "alloc-1",
    transport: {},
    tunnel: {
      upstream: "127.0.0.1:8080",
      proxyPort: 8786,
      ttlSeconds: 1,
      renewEveryMs: 5,
    },
    connectorFactory(config) {
      calls.push(`connector:${config.session.session_id}:${config.clientToken}:${config.upstream}`);
      return {
        async start() {
          calls.push("connector-start");
        },
        async stop() {
          calls.push("connector-stop");
        },
      };
    },
  });

  const metadata = await runtime.start();
  await new Promise((resolve) => setTimeout(resolve, 15));
  assert.equal(metadata.sessionId, "tun-1");
  assert.equal(metadata.boundAddr, "127.0.0.1:8786");
  assert.ok(calls.some((call) => call === "renew:tun-1:client-token"));

  const stopping = runtime.stop();
  await revokeStarted.promise;
  const callCount = calls.length;
  try {
    await new Promise((resolve) => setTimeout(resolve, 15));
    assert.equal(calls.length, callCount);
  } finally {
    revokeComplete.resolve();
    await stopping;
  }
  assert.deepEqual(calls.slice(0, 5), [
    "create:alloc-1:8786",
    "connector:tun-1:client-token:127.0.0.1:8080",
    "connector-start",
    "events:tun-1",
    "get:tun-1",
  ]);
  assert.deepEqual(calls.slice(-2), ["connector-stop", "revoke:tun-1"]);
});

test("tunnel runtime rejects invalid upstream before creating a session", async () => {
  let created = false;
  const control = {
    async createSession() {
      created = true;
      throw new Error("should not create tunnel session");
    },
    async listEvents() {
      return [];
    },
    async getSession() {
      return {};
    },
    async renewSession() {
      return {};
    },
    async revokeSession() {},
  };

  await assert.rejects(
    new TunnelRuntime({
      control,
      allocationId: "alloc-1",
      tunnel: { upstream: "localhost" },
      transport: {},
    }).start(),
    SandboxValidationError,
  );
  assert.equal(created, false);
});

test("sandbox retries the original tunnel revoke after failed close", async (t) => {
  const calls: string[] = [];
  const revokeError = Object.assign(new Error("revoke unavailable"), { code: 14 });
  const notFound = Object.assign(new Error("already revoked"), { code: 5 });
  mockConnector(t, calls);
  const sandbox = sandboxWithTunnel(calls, [revokeError, notFound]);
  await sandbox.start();
  assert.equal(sandbox.metadata.tunnel?.sessionId, "tun-1");

  const firstClose = sandbox.close();
  assert.throws(() => sandbox.metadata, /not started/);
  await assert.rejects(firstClose, (error) => {
    assert.ok(error instanceof AggregateError);
    assert.deepEqual(error.errors, [revokeError]);
    return true;
  });
  await assert.rejects(sandbox.start(), /cleanup is pending/);
  await Promise.all([sandbox.close(), sandbox.close()]);
  assert.deepEqual(calls, [
    "create alloc-1", "connector start", "connector stop",
    "revoke tun-1", "cancel run-1", "delete env-1", "revoke tun-1",
  ]);
});

test("sandbox retains tunnel ownership if startup cleanup cannot revoke it", async (t) => {
  const calls: string[] = [];
  const revokeError = Object.assign(new Error("revoke unavailable"), { code: 14 });
  mockConnector(t, calls);
  const sandbox = sandboxWithTunnel(calls, [revokeError], { readyTimeoutMs: 0 });
  await assert.rejects(sandbox.start(), (error) => {
    assert.ok(error instanceof AggregateError);
    assert.match(String(error.errors[0]), /did not connect client peer/);
    assert.ok(error.errors[1] instanceof AggregateError);
    assert.deepEqual(error.errors[1].errors, [revokeError]);
    return true;
  });
  await assert.rejects(sandbox.start(), /cleanup is pending/);
  await sandbox.close();
  assert.deepEqual(calls, [
    "create alloc-1", "connector start", "connector stop",
    "revoke tun-1", "cancel run-1", "delete env-1", "revoke tun-1",
  ]);
});

test("sandbox revokes an acknowledged tunnel with a missing client token", async () => {
  const calls: string[] = [];
  const sandbox = sandboxWithTunnel(calls, [], { clientToken: "" });
  await assert.rejects(sandbox.start(), /returned no client token/);
  await sandbox.close();
  assert.deepEqual(calls, [
    "create alloc-1", "revoke tun-1", "cancel run-1", "delete env-1",
  ]);
});

function mockConnector(t: TestContext, calls: string[]): void {
  t.mock.method(TunnelConnector.prototype, "start", async () => { calls.push("connector start"); });
  t.mock.method(TunnelConnector.prototype, "stop", async () => { calls.push("connector stop"); });
}

function sandboxWithTunnel(
  calls: string[],
  revokeErrors: Error[],
  options: { readyTimeoutMs?: number; clientToken?: string } = {},
): Sandbox {
  const client = {
    async createEnvironment() { return { id: "env-1" }; },
    async createRun() { return { id: "run-1" }; },
    async *watchRun() { yield { allocation_id: "alloc-1", status: 3 }; },
    async cancelRun(id: string) { calls.push(`cancel ${id}`); },
    async deleteEnvironment(id: string) { calls.push(`delete ${id}`); },
    tunnelTransport() { return {}; },
    tunnelClient() {
      return {
        async createSession({ allocationId }: { allocationId: string }) {
          calls.push(`create ${allocationId}`);
          return { session: { session_id: "tun-1" }, clientToken: options.clientToken ?? "client-token" };
        },
        async listEvents() { return [{ event_type: 6 }]; },
        async getSession() { return { session_id: "tun-1", bound_addr: "127.0.0.1:8786" }; },
        async revokeSession(id: string) {
          calls.push(`revoke ${id}`);
          const error = revokeErrors.shift();
          if (error !== undefined) throw error;
        },
      };
    },
  } as unknown as AxernClient;
  return new Sandbox({
    client,
    image: "python",
    tunnel: { upstream: "127.0.0.1:8080", readyTimeoutMs: options.readyTimeoutMs },
  });
}

test("relay terminal errors are explicit", () => {
  assert.equal(isTerminalRelayError({ code: 16 }), true);
  assert.equal(isTerminalRelayError({ code: 14 }), false);
});
