/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { mapRpcError } from "../errors/index.js";
import { unary } from "../generated/proto.js";
import type { CapabilityStatus, NodeCallOptions } from "../types.js";
import type { NodeClientContext } from "./context.js";

export async function capabilityStatus(ctx: NodeClientContext, options: NodeCallOptions = {}): Promise<CapabilityStatus> {
  try {
    const response = await ctx.withAuthRetry(60, (client) =>
      unary<Record<string, unknown>, Record<string, unknown>>(
        client,
        "CapabilityStatus",
        ctx.authRequest({}),
        options.rpcTimeoutMs,
      ),
    );
    return {
      ready: response.ready === true,
      capabilities: strings(response.capabilities),
      providers: records(response.providers).map((provider) => ({
        name: String(provider.name ?? ""),
        state: String(provider.state ?? ""),
        available: provider.available === true,
        capabilities: strings(provider.capabilities),
        backend: String(provider.backend ?? ""),
        reason: String(provider.reason ?? ""),
        dependencies: records(provider.dependencies).map((dependency) => ({
          name: String(dependency.name ?? ""),
          available: dependency.available === true,
          reason: String(dependency.reason ?? ""),
        })),
      })),
      providerSummary: providerSummary(response.provider_summary),
    };
  } catch (error) {
    throw mapRpcError(error, "sandbox capability status", ctx.allocationId);
  }
}

function providerSummary(input: unknown): CapabilityStatus["providerSummary"] {
  const summary = record(input);
  return {
    total: Number(summary.total ?? 0),
    available: Number(summary.available ?? 0),
    degraded: Number(summary.degraded ?? 0),
    unavailable: Number(summary.unavailable ?? 0),
  };
}

function strings(input: unknown): string[] {
  return Array.isArray(input) ? input.map(String) : [];
}

function records(input: unknown): Record<string, unknown>[] {
  return Array.isArray(input) ? input.map(record) : [];
}

function record(input: unknown): Record<string, unknown> {
  return typeof input === "object" && input !== null ? input as Record<string, unknown> : {};
}
