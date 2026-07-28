/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { mapRpcError } from "../errors/index.js";
import { unary } from "../generated/proto.js";
import type {
  ChmodOptions,
  CopyOptions,
  MkdirOptions,
  MoveOptions,
  RemoveOptions,
  SandboxFileInfo,
  TouchOptions,
  WriteFileOptions,
} from "../types.js";
import { nonEmptyPath } from "../validation.js";
import type { NodeClientContext } from "./context.js";

export async function stat(ctx: NodeClientContext, path: string): Promise<SandboxFileInfo> {
  const response = await fileUnary(ctx, "StatFile", { path: nonEmptyPath(path) });
  return fileInfo(response.info as Record<string, unknown> | undefined);
}

export async function listDir(ctx: NodeClientContext, path: string): Promise<SandboxFileInfo[]> {
  const response = await fileUnary(ctx, "ListDir", { path: nonEmptyPath(path) });
  return ((response.entries as Record<string, unknown>[] | undefined) ?? []).map(fileInfo);
}

export async function exists(ctx: NodeClientContext, path: string): Promise<boolean> {
  const response = await fileUnary(ctx, "Exists", { path: nonEmptyPath(path) });
  return response.exists === true;
}

export async function readFile(ctx: NodeClientContext, path: string): Promise<Buffer> {
  const response = await fileUnary(ctx, "ReadFile", { path: nonEmptyPath(path) });
  return Buffer.from((response.data as Buffer | Uint8Array | undefined) ?? []);
}

export async function writeFile(
  ctx: NodeClientContext,
  path: string,
  data: Buffer | Uint8Array | string,
  options: WriteFileOptions = {},
): Promise<void> {
  await fileUnary(ctx, "WriteFile", {
    path: nonEmptyPath(path),
    data: Buffer.isBuffer(data) ? data : Buffer.from(data),
    create_parents: options.createParents ?? false,
  });
}

export async function mkdir(ctx: NodeClientContext, path: string, options: MkdirOptions = {}): Promise<void> {
  await fileUnary(ctx, "Mkdir", { path: nonEmptyPath(path), parents: options.parents ?? false });
}

export async function remove(ctx: NodeClientContext, path: string, options: RemoveOptions = {}): Promise<void> {
  await fileUnary(ctx, "Remove", {
    path: nonEmptyPath(path),
    recursive: options.recursive ?? false,
    force: options.force ?? false,
  });
}

export async function copy(
  ctx: NodeClientContext,
  srcPath: string,
  dstPath: string,
  options: CopyOptions = {},
): Promise<void> {
  await fileUnary(ctx, "Copy", {
    src_path: nonEmptyPath(srcPath),
    dst_path: nonEmptyPath(dstPath),
    recursive: options.recursive ?? false,
    overwrite: options.overwrite ?? false,
  });
}

export async function move(
  ctx: NodeClientContext,
  srcPath: string,
  dstPath: string,
  options: MoveOptions = {},
): Promise<void> {
  await fileUnary(ctx, "Move", {
    src_path: nonEmptyPath(srcPath),
    dst_path: nonEmptyPath(dstPath),
    overwrite: options.overwrite ?? false,
  });
}

export async function chmod(ctx: NodeClientContext, path: string, mode: number, options: ChmodOptions = {}): Promise<void> {
  await fileUnary(ctx, "Chmod", {
    path: nonEmptyPath(path),
    mode,
    recursive: options.recursive ?? false,
  });
}

export async function touch(ctx: NodeClientContext, path: string, options: TouchOptions = {}): Promise<void> {
  await fileUnary(ctx, "Touch", {
    path: nonEmptyPath(path),
    create: options.create ?? true,
    mtime_ns: options.mtimeNs ?? 0,
  });
}

async function fileUnary(ctx: NodeClientContext, method: string, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  try {
    return await ctx.withAuthRetry(300, (client) =>
      unary(client, method, ctx.authRequest(payload)),
    );
  } catch (error) {
    throw mapRpcError(error, `sandbox ${method}`, ctx.allocationId);
  }
}

function fileInfo(input: Record<string, unknown> | undefined): SandboxFileInfo {
  return {
    path: String(input?.path ?? ""),
    kind: fileKind(input?.kind),
    size: Number(input?.size ?? 0),
    mode: Number(input?.mode ?? 0),
    mtimeNs: Number(input?.mtime_ns ?? 0),
  };
}

function fileKind(kind: unknown): SandboxFileInfo["kind"] {
  switch (kind) {
    case 1:
      return "file";
    case 2:
      return "directory";
    case 3:
      return "symlink";
    case 4:
      return "other";
    default:
      return "unspecified";
  }
}
