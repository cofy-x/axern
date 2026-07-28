/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

export type Dict<T = unknown> = Record<string, T>;

export type Command = string | readonly string[];

export interface ExecOptions {
  env?: Record<string, string>;
  cwd?: string;
  timeoutSeconds?: number;
  user?: string;
  tty?: boolean;
  check?: boolean;
  leaseTtlSeconds?: number;
  rpcTimeoutMs?: number;
}

export interface ExecResult {
  exitCode: number;
  stdout: Buffer;
  stderr: Buffer;
  stdoutTruncated: boolean;
  stderrTruncated: boolean;
  stdoutText(encoding?: BufferEncoding): string;
  stderrText(encoding?: BufferEncoding): string;
}

export interface ImageProcessMount {
  sandboxPath: string;
  targetPath: string;
  readonly?: boolean;
  options?: readonly string[];
}

export interface VolumeMount {
  name: string;
  target: string;
  readonly?: boolean;
  options?: readonly string[];
}

export function workspaceMount(path = "/workspace"): ImageProcessMount {
  return { sandboxPath: path, targetPath: path };
}

export interface ImageExecOptions extends ExecOptions {
  mounts?: readonly ImageProcessMount[];
}

export type ProcessEvent =
  | { kind: "ready" }
  | { kind: "stdout"; data: Buffer }
  | { kind: "stderr"; data: Buffer }
  | { kind: "exit"; exitCode: number; message: string };

export interface ProcessOptions extends Omit<ExecOptions, "check"> {}

export interface ImageProcessOptions extends ProcessOptions {
  mounts?: readonly ImageProcessMount[];
}

export interface ProcessResult {
  exitCode: number;
  message: string;
}

export type SandboxFileKind = "file" | "directory" | "symlink" | "other" | "unspecified";

export interface SandboxFileInfo {
  path: string;
  kind: SandboxFileKind;
  size: number;
  mode: number;
  mtimeNs: number;
}

export interface WriteFileOptions {
  createParents?: boolean;
}

export interface MkdirOptions {
  parents?: boolean;
}

export interface RemoveOptions {
  recursive?: boolean;
  force?: boolean;
}

export interface CopyOptions {
  recursive?: boolean;
  overwrite?: boolean;
}

export interface MoveOptions {
  overwrite?: boolean;
}

export interface ChmodOptions {
  recursive?: boolean;
}

export interface TouchOptions {
  create?: boolean;
  mtimeNs?: number;
}

export interface UploadDirOptions {
  createParents?: boolean;
  overwrite?: boolean;
  chunkSize?: number;
  rpcTimeoutMs?: number;
}

export interface DownloadDirOptions {
  overwrite?: boolean;
  chunkSize?: number;
  rpcTimeoutMs?: number;
}

export interface UploadArchiveOptions {
  createParents?: boolean;
  overwrite?: boolean;
  leaseTtlSeconds?: number;
  rpcTimeoutMs?: number;
}

export interface DownloadArchiveOptions {
  leaseTtlSeconds?: number;
  rpcTimeoutMs?: number;
}

export interface TunnelOptions {
  upstream: string;
  proxyPort?: number;
  ttlSeconds?: number;
  readyTimeoutMs?: number;
  renewEveryMs?: number;
  connector?: TunnelConnectorOptions;
}

export interface TunnelConnectorOptions {
  pingIntervalMs?: number;
  dialTimeoutMs?: number;
  maxStreams?: number;
}

export interface TunnelMetadata {
  sessionId: string;
  boundAddr: string;
  upstream: string;
  proxyPort: number;
}
