/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import type * as grpc from "@grpc/grpc-js";

import { mapRpcError } from "../errors/index.js";
import { unary } from "../generated/proto.js";
import { required } from "../validation.js";
import type { CreateTunnelSessionResult, TunnelSession } from "./types.js";

export class TunnelControlClient {
  constructor(private readonly client: grpc.Client) {}

  async createSession(options: {
    allocationId: string;
    upstream: string;
    proxyPort: number;
    ttlSeconds: number;
    readyTimeoutMs: number;
  }): Promise<CreateTunnelSessionResult> {
    try {
      const response = await unary<Record<string, unknown>, { session?: TunnelSession; client_token?: string }>(
        this.client,
        "CreateTunnelSession",
        {
          allocation_id: required("allocationId", options.allocationId),
          remote_port: options.proxyPort,
          local_target: required("upstream", options.upstream),
          ttl: duration(options.ttlSeconds * 1000),
          wait_ready: true,
          ready_timeout: duration(options.readyTimeoutMs),
        },
      );
      if (response.session === undefined) {
        throw new Error("create tunnel session returned no session");
      }
      return {
        session: response.session,
        clientToken: response.client_token ?? "",
      };
    } catch (error) {
      throw mapRpcError(error, "create tunnel session", options.allocationId);
    }
  }

  async getSession(sessionId: string): Promise<TunnelSession> {
    try {
      const response = await unary<Record<string, unknown>, { session?: TunnelSession }>(
        this.client,
        "GetTunnelSession",
        { session_id: required("sessionId", sessionId) },
      );
      if (response.session === undefined) {
        throw new Error("get tunnel session returned no session");
      }
      return response.session;
    } catch (error) {
      throw mapRpcError(error, "get tunnel session");
    }
  }

  async listEvents(sessionId: string, limit = 30): Promise<Record<string, unknown>[]> {
    try {
      const response = await unary<Record<string, unknown>, { events?: Record<string, unknown>[] }>(
        this.client,
        "ListTunnelSessionEvents",
        { session_id: required("sessionId", sessionId), limit },
      );
      return response.events ?? [];
    } catch (error) {
      throw mapRpcError(error, "list tunnel session events");
    }
  }

  async renewSession(sessionId: string, clientToken: string, ttlSeconds: number): Promise<TunnelSession> {
    try {
      const response = await unary<Record<string, unknown>, { session?: TunnelSession }>(
        this.client,
        "RenewTunnelSession",
        {
          session_id: required("sessionId", sessionId),
          client_token: clientToken,
          ttl: duration(ttlSeconds * 1000),
        },
      );
      if (response.session === undefined) {
        throw new Error("renew tunnel session returned no session");
      }
      return response.session;
    } catch (error) {
      throw mapRpcError(error, "renew tunnel session");
    }
  }

  async revokeSession(sessionId: string, reason = "sdk close"): Promise<void> {
    try {
      await unary(this.client, "RevokeTunnelSession", {
        session_id: required("sessionId", sessionId),
        reason,
      });
    } catch (error) {
      throw mapRpcError(error, "revoke tunnel session");
    }
  }
}

function duration(ms: number): Record<string, number> {
  const seconds = Math.floor(ms / 1000);
  return {
    seconds,
    nanos: Math.floor((ms - seconds * 1000) * 1_000_000),
  };
}
