/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { mapRpcError, SandboxExecError } from "../errors/index.js";
import { unary } from "../generated/proto.js";
import type { Command, ExecOptions, ExecResult, ImageExecOptions, ImageProcessMount, ImageProcessOptions, ProcessOptions } from "../types.js";
import { normalizeCommand } from "../validation.js";
import type { NodeClientContext } from "./context.js";

export async function exec(ctx: NodeClientContext, command: Command, options: ExecOptions = {}): Promise<ExecResult> {
  const argv = normalizeCommand(command);
  try {
    const response = await ctx.withAuthRetry(options.leaseTtlSeconds ?? 300, async (client) =>
      unary<Record<string, unknown>, Record<string, unknown>>(
        client,
        "Exec",
        ctx.authRequest({
          spec: execSpec(argv, options),
        }),
        options.rpcTimeoutMs,
      ),
    );
    const result = execResult(response);
    if (options.check === true && result.exitCode !== 0) {
      throw new SandboxExecError(argv, result);
    }
    return result;
  } catch (error) {
    if (error instanceof SandboxExecError) {
      throw error;
    }
    throw mapRpcError(error, "sandbox exec", ctx.allocationId);
  }
}

export async function execImage(ctx: NodeClientContext, image: string, command: Command, options: ImageExecOptions = {}): Promise<ExecResult> {
  if (image.trim() === "") {
    throw new Error("image is required");
  }
  const argv = normalizeCommand(command);
  try {
    const response = await ctx.withAuthRetry(options.leaseTtlSeconds ?? 300, async (client) =>
      unary<Record<string, unknown>, Record<string, unknown>>(
        client,
        "ExecImage",
        ctx.authRequest({
          spec: imageProcessSpec(image, argv, options),
        }),
        options.rpcTimeoutMs,
      ),
    );
    const result = execResult(response);
    if (options.check === true && result.exitCode !== 0) {
      throw new SandboxExecError(argv, result);
    }
    return result;
  } catch (error) {
    if (error instanceof SandboxExecError) {
      throw error;
    }
    throw mapRpcError(error, "sandbox exec image", ctx.allocationId);
  }
}

export function execSpec(argv: string[], options: ExecOptions | ProcessOptions): Record<string, unknown> {
  return {
    argv,
    env: options.env ?? {},
    cwd: options.cwd ?? "",
    timeout_seconds: options.timeoutSeconds ?? 0,
    tty: options.tty ?? false,
    user: options.user ?? "",
  };
}

export function imageProcessSpec(image: string, argv: string[], options: ImageExecOptions | ImageProcessOptions): Record<string, unknown> {
  return {
    image,
    argv,
    env: options.env ?? {},
    cwd: options.cwd ?? "",
    timeout_seconds: options.timeoutSeconds ?? 0,
    tty: options.tty ?? false,
    user: options.user ?? "",
    mounts: imageProcessMounts(options.mounts),
  };
}

function imageProcessMounts(mounts: readonly ImageProcessMount[] | undefined): Record<string, unknown>[] {
  const effective = mounts ?? [{ sandboxPath: "/workspace", targetPath: "/workspace" }];
  return effective.map((mount) => ({
    sandbox_path: mount.sandboxPath,
    target_path: mount.targetPath,
    readonly: mount.readonly ?? false,
    options: [...(mount.options ?? [])],
  }));
}

export function execResult(response: Record<string, unknown>): ExecResult {
  const stdout = Buffer.from((response.stdout as Buffer | Uint8Array | undefined) ?? []);
  const stderr = Buffer.from((response.stderr as Buffer | Uint8Array | undefined) ?? []);
  return {
    exitCode: Number(response.exit_code ?? 0),
    stdout,
    stderr,
    stdoutTruncated: response.stdout_truncated === true,
    stderrTruncated: response.stderr_truncated === true,
    stdoutText: (encoding: BufferEncoding = "utf8") => stdout.toString(encoding),
    stderrText: (encoding: BufferEncoding = "utf8") => stderr.toString(encoding),
  };
}
