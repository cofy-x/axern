/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import type * as grpc from "@grpc/grpc-js";

import { mapRpcError } from "../errors/index.js";
import type { Command, ImageProcessOptions, ProcessOptions } from "../types.js";
import { normalizeCommand } from "../validation.js";
import { NodeClientContext } from "./context.js";
import { execSpec, imageProcessSpec } from "./exec.js";
import { SandboxProcess } from "./process.js";

export async function process(ctx: NodeClientContext, command: Command, options: ProcessOptions = {}): Promise<SandboxProcess> {
  const argv = normalizeCommand(command);
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

export async function processImage(ctx: NodeClientContext, image: string, command: Command, options: ImageProcessOptions = {}): Promise<SandboxProcess> {
  if (image.trim() === "") {
    throw new Error("image is required");
  }
  const argv = normalizeCommand(command);
  const client = ctx.rpcClient();
  const call = (client as unknown as { ProcessImage(): grpc.ClientDuplexStream<Record<string, unknown>, Record<string, unknown>> })
    .ProcessImage();
  const sandboxProcess = new SandboxProcess({
    allocationId: ctx.allocationId,
    call,
    closeClient: () => client.close(),
  });
  call.write({
    open: ctx.authRequest({
      spec: imageProcessSpec(image, argv, options),
    }),
  });
  try {
    await sandboxProcess.waitReady();
    return sandboxProcess;
  } catch (error) {
    await sandboxProcess.close();
    throw mapRpcError(error, "sandbox image process", ctx.allocationId);
  }
}
