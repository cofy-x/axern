/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { SandboxValidationError } from "../src/errors/index.js";
import { isTerminalRelayError } from "../src/tunnel/relay.js";
import { startTunnelRuntime } from "../src/tunnel/runtime.js";

test("tunnel runtime starts connector, waits for client event, renews, and revokes", async () => {
  const calls: string[] = [];
  const control = {
    async createSession(options: Record<string, unknown>) {
      calls.push(`create:${String(options.allocationId)}:${String(options.upstream)}:${String(options.proxyPort)}`);
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
    },
  };

  const runtime = await startTunnelRuntime({
    control,
    allocationId: "alloc-1",
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

  await new Promise((resolve) => setTimeout(resolve, 15));
  assert.equal(runtime.sessionId, "tun-1");
  assert.equal(runtime.boundAddr, "127.0.0.1:8786");
  assert.ok(calls.some((call) => call === "renew:tun-1:client-token"));

  await runtime.stop();
  assert.deepEqual(calls.slice(0, 5), [
    "create:alloc-1:127.0.0.1:8080:8786",
    "connector:tun-1:client-token:127.0.0.1:8080",
    "connector-start",
    "events:tun-1",
    "get:tun-1",
  ]);
  assert.ok(calls.includes("connector-stop"));
  assert.ok(calls.includes("revoke:tun-1"));
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
    startTunnelRuntime({
      control,
      allocationId: "alloc-1",
      tunnel: { upstream: "localhost" },
    }),
    SandboxValidationError,
  );
  assert.equal(created, false);
});

test("relay terminal errors are explicit", () => {
  assert.equal(isTerminalRelayError({ code: 16 }), true);
  assert.equal(isTerminalRelayError({ code: 14 }), false);
});
