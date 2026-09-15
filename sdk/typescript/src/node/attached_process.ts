/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import type * as grpc from "@grpc/grpc-js";

import { mapRpcError, SandboxValidationError } from "../errors/index.js";
import type { Command, ProcessOptions } from "../types.js";
import { normalizeCommand } from "../validation.js";
import { NodeClientContext } from "./context.js";
import { execSpec } from "./exec.js";
import { SandboxProcess } from "./process.js";

export async function process(ctx: NodeClientContext, command: Command, options: ProcessOptions = {}): Promise<SandboxProcess> {
  const argv = normalizeCommand(command);
  const initialSize = processInitialSize(options);
  const client = ctx.rpcClient();
  const call = (client as unknown as { Process(): grpc.ClientDuplexStream<Record<string, unknown>, Record<string, unknown>> })
    .Process();
  const sandboxProcess = new SandboxProcess({
    allocationId: ctx.allocationId,
    call,
    closeClient: () => client.close(),
  });
  call.write({
    open: ctx.authRequest({
      spec: execSpec(argv, options),
      ...(initialSize === undefined ? {} : { initial_size: initialSize }),
    }),
  });
  try {
    await sandboxProcess.waitReady();
    return sandboxProcess;
  } catch (error) {
    await sandboxProcess.close();
    throw mapRpcError(error, "sandbox process", ctx.allocationId);
  }
}

function processInitialSize(options: ProcessOptions): { cols: number; rows: number } | undefined {
  const cols = options.initialCols ?? 0;
  const rows = options.initialRows ?? 0;
  if ((cols === 0) !== (rows === 0)) {
    throw new SandboxValidationError("initialCols and initialRows must both be zero or both be positive");
  }
  if (cols < 0 || rows < 0 || !Number.isInteger(cols) || !Number.isInteger(rows)) {
    throw new SandboxValidationError("initialCols and initialRows must be non-negative integers");
  }
  return cols === 0 ? undefined : { cols, rows };
}
