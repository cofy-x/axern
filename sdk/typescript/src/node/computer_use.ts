/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { mapRpcError } from "../errors/index.js";
import { unary } from "../generated/proto.js";
import type {
  ComputerUseDisplay,
  ComputerUseKeyboardOptions,
  ComputerUseMouseOptions,
  ComputerUseScreenshot,
  ComputerUseScreenshotOptions,
  ComputerUseStatus,
  NodeCallOptions,
} from "../types.js";
import type { NodeClientContext } from "./context.js";

export async function computerUseStatus(ctx: NodeClientContext, options: NodeCallOptions = {}): Promise<ComputerUseStatus> {
  const response = await computerUseUnary(ctx, "ComputerUseStatus", {}, options.rpcTimeoutMs);
  return {
    available: response.available === true,
    display: String(response.display ?? ""),
    backend: String(response.backend ?? ""),
    reason: String(response.reason ?? ""),
    dependencies: records(response.dependencies).map((dependency) => ({
      name: String(dependency.name ?? ""),
      available: dependency.available === true,
      reason: String(dependency.reason ?? ""),
    })),
  };
}

export async function computerUseScreenshot(
  ctx: NodeClientContext,
  options: ComputerUseScreenshotOptions = {},
): Promise<ComputerUseScreenshot> {
  const response = await computerUseUnary(ctx, "ComputerUseScreenshot", {
    show_cursor: options.showCursor ?? false,
    region: options.region === undefined ? undefined : {
      x: options.region.x,
      y: options.region.y,
      width: options.region.width,
      height: options.region.height,
    },
    format: options.format ?? "",
    quality: options.quality ?? 0,
    scale: options.scale ?? 0,
  }, options.rpcTimeoutMs);
  return {
    data: Buffer.from((response.data as Buffer | Uint8Array | undefined) ?? []),
    contentType: String(response.content_type ?? ""),
  };
}

export async function computerUseDisplay(ctx: NodeClientContext, options: NodeCallOptions = {}): Promise<ComputerUseDisplay> {
  const response = await computerUseUnary(ctx, "ComputerUseDisplay", {}, options.rpcTimeoutMs);
  return {
    display: String(response.display ?? ""),
    backend: String(response.backend ?? ""),
    width: Number(response.width ?? 0),
    height: Number(response.height ?? 0),
  };
}

export async function computerUseMouse(ctx: NodeClientContext, options: ComputerUseMouseOptions = {}): Promise<void> {
  await computerUseUnary(ctx, "ComputerUseMouse", {
    action: options.action ?? "click",
    x: options.x ?? 0,
    y: options.y ?? 0,
    to_x: options.toX ?? 0,
    to_y: options.toY ?? 0,
    button: options.button ?? "",
    direction: options.direction ?? "",
    amount: options.amount ?? 0,
  }, options.rpcTimeoutMs);
}

export async function computerUseKeyboard(ctx: NodeClientContext, options: ComputerUseKeyboardOptions = {}): Promise<void> {
  await computerUseUnary(ctx, "ComputerUseKeyboard", {
    text: options.text ?? "",
    key: options.key ?? "",
    keys: options.keys ?? [],
    delay_ms: options.delayMs ?? 0,
  }, options.rpcTimeoutMs);
}

async function computerUseUnary(
  ctx: NodeClientContext,
  method: string,
  payload: Record<string, unknown>,
  rpcTimeoutMs?: number,
): Promise<Record<string, unknown>> {
  try {
    return await ctx.withAuthRetry(60, (client) =>
      unary(client, method, ctx.authRequest(payload), rpcTimeoutMs),
    );
  } catch (error) {
    throw mapRpcError(error, `sandbox ${method}`, ctx.allocationId);
  }
}

function records(input: unknown): Record<string, unknown>[] {
  return Array.isArray(input)
    ? input.map((item) => typeof item === "object" && item !== null ? item as Record<string, unknown> : {})
    : [];
}
