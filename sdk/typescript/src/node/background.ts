/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { mapRpcError } from "../errors/index.js";
import { unary } from "../generated/proto.js";
import type { Command, NodeCallOptions } from "../types.js";
import { normalizeCommand } from "../validation.js";
import type { NodeClientContext } from "./context.js";
import { execSpec } from "./exec.js";

export interface BackgroundProcessOptions extends NodeCallOptions {
  timeoutSeconds: number;
  callerRef?: string;
  env?: Record<string, string>;
  cwd?: string;
  user?: string;
}

export interface BackgroundProcess {
  processId: string;
  state: "starting" | "running" | "exited" | "failed" | "unspecified";
  exitCode?: number;
  callerRef: string;
  startedAt?: Date;
  finishedAt?: Date;
}

type WireTimestamp = { seconds?: string | number; nanos?: number };

type WireProcess = {
  process_id?: string;
  state?: number;
  exit_code?: number;
  caller_ref?: string;
  started_at?: WireTimestamp;
  finished_at?: WireTimestamp;
};

type ProcessResponse = { process: WireProcess };

function fromTimestamp(value: WireTimestamp | undefined): Date | undefined {
  return value === undefined ? undefined : new Date(Number(value.seconds ?? 0) * 1000 + (value.nanos ?? 0) / 1_000_000);
}

const states = ["unspecified", "starting", "running", "exited", "failed"] as const;

function fromProcess(process: WireProcess): BackgroundProcess {
  return {
    processId: process.process_id ?? "",
    state: states[process.state ?? 0] ?? "unspecified",
    exitCode: process.exit_code,
    callerRef: process.caller_ref ?? "",
    startedAt: fromTimestamp(process.started_at),
    finishedAt: fromTimestamp(process.finished_at),
  };
}

async function call<T>(ctx: NodeClientContext, method: string, payload: Record<string, unknown>, options: NodeCallOptions): Promise<T> {
  try {
    return await ctx.withAuthRetry((client) => unary<Record<string, unknown>, T>(
      client, method, ctx.authRequest(payload), options.rpcTimeoutMs,
    ));
  } catch (error) {
    throw mapRpcError(error, method, ctx.allocationId);
  }
}

export async function startBackground(ctx: NodeClientContext, command: Command, options: BackgroundProcessOptions): Promise<BackgroundProcess> {
  if (!Number.isInteger(options.timeoutSeconds) || options.timeoutSeconds < 1 || options.timeoutSeconds > 3600) {
    throw new TypeError("background process timeout must be a whole number of seconds from 1 through 3600");
  }
  const response = await call<ProcessResponse>(ctx, "StartBackgroundProcess", {
    spec: execSpec(normalizeCommand(command), options),
    caller_ref: options.callerRef ?? "",
  }, options);
  return fromProcess(response.process);
}

export async function getBackground(ctx: NodeClientContext, processId: string, options: NodeCallOptions): Promise<BackgroundProcess> {
  return fromProcess((await call<ProcessResponse>(ctx, "GetBackgroundProcess", { process_id: processId }, options)).process);
}

export async function listBackground(ctx: NodeClientContext, options: NodeCallOptions): Promise<BackgroundProcess[]> {
  const response = await call<{ processes?: WireProcess[] }>(ctx, "ListBackgroundProcesses", {}, options);
  return (response.processes ?? []).map(fromProcess);
}

export async function cancelBackground(ctx: NodeClientContext, processId: string, options: NodeCallOptions): Promise<BackgroundProcess> {
  return fromProcess((await call<ProcessResponse>(ctx, "CancelBackgroundProcess", { process_id: processId }, options)).process);
}
