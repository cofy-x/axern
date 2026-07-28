/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { SandboxValidationError } from "../errors/index.js";
import type { TunnelOptions, TunnelMetadata } from "../types.js";
import { TunnelConnector } from "./connector.js";
import type { TunnelConnectorConfig } from "./connector.js";
import type { GatewayTransportOptions } from "./relay.js";
import { parseTcpTarget } from "./target.js";
import type { CreateTunnelSessionResult, TunnelRuntime, TunnelSession } from "./types.js";

const defaultProxyPort = 8786;
const defaultTtlSeconds = 300;
const defaultReadyTimeoutMs = 30_000;

export interface TunnelConnectorRunner {
  start(): Promise<void>;
  stop(): Promise<void>;
}

export interface TunnelControl {
  createSession(options: {
    allocationId: string;
    upstream: string;
    proxyPort: number;
    ttlSeconds: number;
    readyTimeoutMs: number;
  }): Promise<CreateTunnelSessionResult>;
  getSession(sessionId: string): Promise<TunnelSession>;
  listEvents(sessionId: string, limit?: number): Promise<Record<string, unknown>[]>;
  renewSession(sessionId: string, clientToken: string, ttlSeconds: number): Promise<TunnelSession>;
  revokeSession(sessionId: string, reason?: string): Promise<void>;
}

export async function startTunnelRuntime(options: {
  control: TunnelControl;
  allocationId: string;
  tunnel: TunnelOptions;
  transport: GatewayTransportOptions;
  connectorFactory?: (config: TunnelConnectorConfig) => TunnelConnectorRunner;
}): Promise<TunnelRuntime> {
  validateTunnelOptions(options.tunnel);
  const ttlSeconds = options.tunnel.ttlSeconds ?? defaultTtlSeconds;
  const proxyPort = options.tunnel.proxyPort ?? defaultProxyPort;
  const result = await options.control.createSession({
    allocationId: options.allocationId,
    upstream: options.tunnel.upstream,
    proxyPort,
    ttlSeconds,
    readyTimeoutMs: options.tunnel.readyTimeoutMs ?? defaultReadyTimeoutMs,
  });
  const sessionId = result.session.session_id ?? "";
  if (sessionId === "") {
    throw new Error("create tunnel session returned no session id");
  }
  if (result.clientToken === "") {
    throw new Error(`tunnel session ${sessionId} returned no client token`);
  }
  const connectorConfig = {
    session: result.session,
    clientToken: result.clientToken,
    upstream: options.tunnel.upstream,
    transport: options.transport,
    connector: options.tunnel.connector,
  };
  const connector = (options.connectorFactory ?? ((config) => new TunnelConnector(config)))(connectorConfig);
  let timer: NodeJS.Timeout | undefined;
  try {
    await connector.start();
    await waitClientConnected(options.control, sessionId, options.tunnel.readyTimeoutMs ?? defaultReadyTimeoutMs);
    const session = await options.control.getSession(sessionId);
    const boundAddr = session.bound_addr ?? "";
    if (boundAddr === "") {
      throw new Error(`tunnel session ${sessionId} has no bound address`);
    }
    const renewEveryMs = options.tunnel.renewEveryMs ?? Math.max(30_000, Math.floor((ttlSeconds * 1000) / 2));
    timer = setInterval(() => {
      void options.control.renewSession(sessionId, result.clientToken, ttlSeconds).catch(() => undefined);
    }, renewEveryMs);
    timer.unref();
    return {
      sessionId,
      clientToken: result.clientToken,
      boundAddr,
      upstream: options.tunnel.upstream,
      proxyPort,
      async stop() {
        if (timer !== undefined) {
          clearInterval(timer);
          timer = undefined;
        }
        await connector.stop();
        await options.control.revokeSession(sessionId).catch(() => undefined);
      },
    };
  } catch (error) {
    if (timer !== undefined) {
      clearInterval(timer);
    }
    await connector.stop().catch(() => undefined);
    if (sessionId !== "") {
      await options.control.revokeSession(sessionId).catch(() => undefined);
    }
    throw error;
  }
}

export function tunnelMetadata(runtime: TunnelRuntime | undefined): TunnelMetadata | undefined {
  if (runtime === undefined) {
    return undefined;
  }
  return {
    sessionId: runtime.sessionId,
    boundAddr: runtime.boundAddr,
    upstream: runtime.upstream,
    proxyPort: runtime.proxyPort,
  };
}

function validateTunnelOptions(options: TunnelOptions): void {
  if (options.upstream.trim() === "") {
    throw new SandboxValidationError("tunnel.upstream is required");
  }
  parseTcpTarget(options.upstream);
  if (options.proxyPort !== undefined && options.proxyPort < 0) {
    throw new SandboxValidationError("tunnel.proxyPort must be non-negative");
  }
  if (options.ttlSeconds !== undefined && options.ttlSeconds < 0) {
    throw new SandboxValidationError("tunnel.ttlSeconds must be non-negative");
  }
  if (options.readyTimeoutMs !== undefined && options.readyTimeoutMs < 0) {
    throw new SandboxValidationError("tunnel.readyTimeoutMs must be non-negative");
  }
}

async function waitClientConnected(control: TunnelControl, sessionId: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const events = await control.listEvents(sessionId, 30);
    if (events.some((event) => Number(event.event_type ?? 0) === 6)) {
      return;
    }
    await sleep(500);
  }
  throw new Error(`tunnel session ${sessionId} did not connect client peer within ${timeoutMs}ms`);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
