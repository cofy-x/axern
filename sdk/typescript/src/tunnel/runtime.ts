/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { isNotFound, SandboxValidationError } from "../errors/index.js";
import type { TunnelOptions, TunnelMetadata } from "../types.js";
import { TunnelConnector } from "./connector.js";
import type { TunnelConnectorConfig } from "./connector.js";
import type { GatewayTransportOptions } from "./relay.js";
import { parseTcpTarget } from "./target.js";
import type { CreateTunnelSessionResult, TunnelSession } from "./types.js";

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
    proxyPort: number;
    ttlSeconds: number;
    readyTimeoutMs: number;
  }): Promise<CreateTunnelSessionResult>;
  getSession(sessionId: string): Promise<TunnelSession>;
  listEvents(sessionId: string, limit?: number): Promise<Record<string, unknown>[]>;
  renewSession(sessionId: string, clientToken: string, ttlSeconds: number): Promise<TunnelSession>;
  revokeSession(sessionId: string, reason?: string): Promise<void>;
}

interface TunnelRuntimeOptions {
  control: TunnelControl;
  allocationId: string;
  tunnel: TunnelOptions;
  transport: GatewayTransportOptions;
  connectorFactory?: (config: TunnelConnectorConfig) => TunnelConnectorRunner;
}

export class TunnelRuntime {
  private sessionId?: string;
  private connector?: TunnelConnectorRunner;
  private renewal?: NodeJS.Timeout;

  constructor(private readonly options: TunnelRuntimeOptions) {}

  async start(): Promise<TunnelMetadata> {
    const { control, allocationId, tunnel, transport, connectorFactory } = this.options;
    validateTunnelOptions(tunnel);
    const ttlSeconds = tunnel.ttlSeconds ?? defaultTtlSeconds;
    const proxyPort = tunnel.proxyPort ?? defaultProxyPort;
    const readyTimeoutMs = tunnel.readyTimeoutMs ?? defaultReadyTimeoutMs;
    const result = await control.createSession({ allocationId, proxyPort, ttlSeconds, readyTimeoutMs });
    const sessionId = result.session.session_id ?? "";
    if (sessionId === "") {
      throw new Error("create tunnel session returned no session id");
    }
    this.sessionId = sessionId;
    if (result.clientToken === "") {
      throw new Error(`tunnel session ${sessionId} returned no client token`);
    }
    const connectorConfig = {
      session: result.session,
      clientToken: result.clientToken,
      upstream: tunnel.upstream,
      transport,
      connector: tunnel.connector,
    };
    this.connector = connectorFactory === undefined ? new TunnelConnector(connectorConfig) : connectorFactory(connectorConfig);
    await this.connector.start();
    await waitClientConnected(control, sessionId, readyTimeoutMs);
    const session = await control.getSession(sessionId);
    const boundAddr = session.bound_addr ?? "";
    if (boundAddr === "") {
      throw new Error(`tunnel session ${sessionId} has no bound address`);
    }
    const renewEveryMs = tunnel.renewEveryMs ?? Math.max(30_000, Math.floor((ttlSeconds * 1000) / 2));
    this.renewal = setInterval(() => {
      void control.renewSession(sessionId, result.clientToken, ttlSeconds).catch(() => undefined);
    }, renewEveryMs);
    this.renewal.unref();
    return { sessionId, boundAddr, upstream: tunnel.upstream, proxyPort };
  }

  async stop(): Promise<void> {
    clearInterval(this.renewal);
    this.renewal = undefined;
    const errors: unknown[] = [];
    try {
      await this.connector?.stop();
      this.connector = undefined;
    } catch (error) {
      errors.push(error);
    }
    if (this.sessionId !== undefined) {
      try {
        await this.options.control.revokeSession(this.sessionId);
        this.sessionId = undefined;
      } catch (error) {
        if (isNotFound(error)) this.sessionId = undefined;
        else errors.push(error);
      }
    }
    if (errors.length === 1) throw errors[0];
    if (errors.length > 1) throw new AggregateError(errors, "tunnel cleanup failed");
  }
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
