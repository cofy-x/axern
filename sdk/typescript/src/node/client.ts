/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import type * as grpc from "@grpc/grpc-js";

import type {
  ChmodOptions,
  Command,
  CopyOptions,
  DownloadArchiveOptions,
  ExecOptions,
  ExecResult,
  ImageExecOptions,
  ImageProcessOptions,
  MkdirOptions,
  MoveOptions,
  ProcessOptions,
  RemoveOptions,
  SandboxFileInfo,
  TouchOptions,
  UploadArchiveOptions,
  WriteFileOptions,
} from "../types.js";
import { uploadArchive, downloadArchive } from "./archive.js";
import { process as startProcess } from "./attached_process.js";
import { processImage as startImageProcess } from "./attached_process.js";
import { NodeClientContext } from "./context.js";
import { exec, execImage } from "./exec.js";
import * as files from "./files.js";
import type { SandboxProcess } from "./process.js";

export interface NodeSandboxClientOptions {
  allocationId: string;
  target: string;
  credentials: grpc.ChannelCredentials;
  channelOptions?: grpc.ChannelOptions;
}

export class NodeSandboxClient {
  readonly allocationId: string;

  private readonly ctx: NodeClientContext;

  constructor(options: NodeSandboxClientOptions) {
    this.allocationId = options.allocationId;
    this.ctx = new NodeClientContext(options);
  }

  async exec(command: Command, options: ExecOptions = {}): Promise<ExecResult> {
    return exec(this.ctx, command, options);
  }

  async process(command: Command, options: ProcessOptions = {}): Promise<SandboxProcess> {
    return startProcess(this.ctx, command, options);
  }

  async execImage(image: string, command: Command, options: ImageExecOptions = {}): Promise<ExecResult> {
    return execImage(this.ctx, image, command, options);
  }

  async processImage(image: string, command: Command, options: ImageProcessOptions = {}): Promise<SandboxProcess> {
    return startImageProcess(this.ctx, image, command, options);
  }

  async stat(path: string): Promise<SandboxFileInfo> {
    return files.stat(this.ctx, path);
  }

  async listDir(path: string): Promise<SandboxFileInfo[]> {
    return files.listDir(this.ctx, path);
  }

  async exists(path: string): Promise<boolean> {
    return files.exists(this.ctx, path);
  }

  async readFile(path: string): Promise<Buffer> {
    return files.readFile(this.ctx, path);
  }

  async readText(path: string, encoding: BufferEncoding = "utf8"): Promise<string> {
    return (await this.readFile(path)).toString(encoding);
  }

  async writeFile(path: string, data: Buffer | Uint8Array | string, options: WriteFileOptions = {}): Promise<void> {
    return files.writeFile(this.ctx, path, data, options);
  }

  async writeText(path: string, data: string, options: WriteFileOptions = {}): Promise<void> {
    return this.writeFile(path, data, options);
  }

  async mkdir(path: string, options: MkdirOptions = {}): Promise<void> {
    return files.mkdir(this.ctx, path, options);
  }

  async remove(path: string, options: RemoveOptions = {}): Promise<void> {
    return files.remove(this.ctx, path, options);
  }

  async copy(srcPath: string, dstPath: string, options: CopyOptions = {}): Promise<void> {
    return files.copy(this.ctx, srcPath, dstPath, options);
  }

  async move(srcPath: string, dstPath: string, options: MoveOptions = {}): Promise<void> {
    return files.move(this.ctx, srcPath, dstPath, options);
  }

  async chmod(path: string, mode: number, options: ChmodOptions = {}): Promise<void> {
    return files.chmod(this.ctx, path, mode, options);
  }

  async touch(path: string, options: TouchOptions = {}): Promise<void> {
    return files.touch(this.ctx, path, options);
  }

  async uploadArchive(path: string, chunks: () => AsyncIterable<Buffer | Uint8Array>, options: UploadArchiveOptions = {}): Promise<void> {
    return uploadArchive(this.ctx, path, chunks, options);
  }

  downloadArchive(path: string, options: DownloadArchiveOptions = {}): AsyncIterable<Buffer> {
    return downloadArchive(this.ctx, path, options);
  }
}
