/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { AxernClient } from "../client/index.js";
import type { DeclaredOutput, ExtensionCapability } from "../client/index.js";
import { SandboxStateError } from "../errors/index.js";
import type { AllocationClient } from "../node/client.js";
import type { SandboxProcess } from "../node/process.js";
import type { ResourceQuantity } from "../resources.js";
import type { NetworkPolicy } from "../network-policy.js";
import { startTunnelRuntime, tunnelMetadata } from "../tunnel/runtime.js";
import type { TunnelRuntime } from "../tunnel/types.js";
import type {
  ChmodOptions,
  CapabilityStatus,
  Command,
  ComputerUseDisplay,
  ComputerUseKeyboardOptions,
  ComputerUseMouseOptions,
  ComputerUseScreenshot,
  ComputerUseScreenshotOptions,
  ComputerUseStatus,
  CopyOptions,
  DownloadDirOptions,
  ExecOptions,
  ExecResult,
  MkdirOptions,
  MoveOptions,
  NodeCallOptions,
  ProcessOptions,
  RemoveOptions,
  SandboxFileInfo,
  TunnelMetadata,
  TunnelOptions,
  TouchOptions,
  UploadDirOptions,
  WriteFileOptions,
} from "../types.js";
import { directoryArchiveChunks, extractDirectoryArchive } from "./archive.js";
import { defaultSandboxArgv, sandboxLabels, sandboxMetadata, validateSandboxOptions, waitRunningRun } from "./lifecycle.js";

export interface SandboxOptions {
  client: AxernClient;
  templateId?: string;
  image?: string;
  environmentId?: string;
  namespace?: string;
  argv?: string[];
  env?: Record<string, string>;
  cwd?: string;
  networkPolicy?: NetworkPolicy;
  extensionCapabilities?: readonly ExtensionCapability[];
  declaredOutputs?: readonly DeclaredOutput[];
  requestCpu?: ResourceQuantity;
  requestMemory?: ResourceQuantity;
  requestEphemeralStorage?: ResourceQuantity;
  limitCpu?: ResourceQuantity;
  limitMemory?: ResourceQuantity;
  limitEphemeralStorage?: ResourceQuantity;
  readyTimeoutMs?: number;
  labels?: Record<string, string>;
  registryCredentialId?: string;
  rootfsReadonly?: boolean;
  tunnel?: TunnelOptions;
}

export interface SandboxState {
  environmentId: string;
  runId: string;
  allocationId: string;
  startedAt: Date;
}

export interface SandboxMetadata extends SandboxState {
  namespace: string;
  labels: Record<string, string>;
  source: "template" | "image" | "environment";
  tunnel?: TunnelMetadata;
}

export class Sandbox {
  private readonly client: AxernClient;
  private readonly options: SandboxOptions;
  private createdEnvironment = false;
  private environmentId = "";
  private runId = "";
  private currentState?: SandboxState;
  private currentMetadata?: SandboxMetadata;
  private tunnelRuntime?: TunnelRuntime;

  constructor(options: SandboxOptions) {
    validateSandboxOptions(options);
    this.client = options.client;
    this.options = options;
  }

  get state(): SandboxState {
    if (this.currentState === undefined) {
      throw new SandboxStateError("sandbox is not started");
    }
    return this.currentState;
  }

  get metadata(): SandboxMetadata {
    if (this.currentMetadata === undefined) {
      throw new SandboxStateError("sandbox is not started");
    }
    return this.currentMetadata;
  }

  async start(): Promise<this> {
    if (this.currentState !== undefined) {
      return this;
    }
    let environmentId = this.options.environmentId ?? "";
    try {
      if (environmentId === "") {
        const environment = await this.client.createEnvironment({
          namespace: this.options.namespace,
          templateId: this.options.templateId,
          image: this.options.image,
          registryCredentialId: this.options.registryCredentialId,
          rootfsReadonly: this.options.rootfsReadonly,
          labels: sandboxLabels(this.options.labels),
        });
        environmentId = String(environment.id ?? "");
        this.environmentId = environmentId;
        this.createdEnvironment = true;
      }
      const run = await this.client.createRun({
        namespace: this.options.namespace,
        environmentId,
        argv: this.options.argv ?? defaultSandboxArgv,
        env: this.options.env,
        cwd: this.options.cwd,
        networkPolicy: this.options.networkPolicy,
        extensionCapabilities: this.options.extensionCapabilities,
        declaredOutputs: this.options.declaredOutputs,
        requestCpu: this.options.requestCpu,
        requestMemory: this.options.requestMemory,
        requestEphemeralStorage: this.options.requestEphemeralStorage,
        limitCpu: this.options.limitCpu,
        limitMemory: this.options.limitMemory,
        limitEphemeralStorage: this.options.limitEphemeralStorage,
        labels: sandboxLabels(this.options.labels),
      });
      this.runId = String(run.id ?? "");
      const runningRun = await waitRunningRun(
        this.runId,
        this.options.readyTimeoutMs ?? 180_000,
        (runId, signal) => this.client.watchRun(runId, { signal }),
      );
      this.currentState = {
        environmentId,
        runId: this.runId,
        allocationId: String(runningRun.allocation_id ?? ""),
        startedAt: new Date(),
      };
      this.currentMetadata = sandboxMetadata(this.options, this.currentState);
      if (this.options.tunnel !== undefined) {
        this.tunnelRuntime = await startTunnelRuntime({
          control: this.client.tunnelClient(),
          allocationId: this.currentState.allocationId,
          tunnel: this.options.tunnel,
          transport: this.client.tunnelTransport(),
        });
        this.currentMetadata = {
          ...this.currentMetadata,
          tunnel: tunnelMetadata(this.tunnelRuntime),
        };
      }
      return this;
    } catch (error) {
      try {
        await this.close();
      } catch (cleanupError) {
        throw new AggregateError([error, cleanupError], "sandbox start and cleanup failed");
      }
      throw error;
    }
  }

  async close(): Promise<void> {
    const errors: unknown[] = [];
    const runId = this.runId;
    const tunnelRuntime = this.tunnelRuntime;
    this.runId = "";
    this.tunnelRuntime = undefined;
    this.currentState = undefined;
    this.currentMetadata = undefined;
    if (tunnelRuntime !== undefined) {
      await tunnelRuntime.stop().catch((error: unknown) => errors.push(error));
    }
    if (runId !== "") {
      await this.client.cancelRun(runId).catch((error: unknown) => errors.push(error));
    }
    if (this.createdEnvironment && this.environmentId !== "") {
      const environmentId = this.environmentId;
      this.environmentId = "";
      this.createdEnvironment = false;
      await this.client.deleteEnvironment(environmentId).catch((error: unknown) => errors.push(error));
    }
    if (errors.length > 0) throw new AggregateError(errors, "sandbox cleanup failed");
  }

  async exec(command: Command, options: ExecOptions = {}): Promise<ExecResult> {
    return this.nodeClient().exec(command, options);
  }

  async process(command: Command, options: ProcessOptions = {}): Promise<SandboxProcess> {
    return this.nodeClient().process(command, options);
  }



  async capabilityStatus(options: NodeCallOptions = {}): Promise<CapabilityStatus> {
    return this.nodeClient().capabilityStatus(options);
  }

  async computerUseStatus(options: NodeCallOptions = {}): Promise<ComputerUseStatus> {
    return this.nodeClient().computerUseStatus(options);
  }

  async computerUseScreenshot(options: ComputerUseScreenshotOptions = {}): Promise<ComputerUseScreenshot> {
    return this.nodeClient().computerUseScreenshot(options);
  }

  async computerUseDisplay(options: NodeCallOptions = {}): Promise<ComputerUseDisplay> {
    return this.nodeClient().computerUseDisplay(options);
  }

  async computerUseMouse(options: ComputerUseMouseOptions = {}): Promise<void> {
    return this.nodeClient().computerUseMouse(options);
  }

  async computerUseKeyboard(options: ComputerUseKeyboardOptions = {}): Promise<void> {
    return this.nodeClient().computerUseKeyboard(options);
  }

  async stat(path: string): Promise<SandboxFileInfo> {
    return this.nodeClient().stat(path);
  }

  async listDir(path: string): Promise<SandboxFileInfo[]> {
    return this.nodeClient().listDir(path);
  }

  async exists(path: string): Promise<boolean> {
    return this.nodeClient().exists(path);
  }

  async readFile(path: string): Promise<Buffer> {
    return this.nodeClient().readFile(path);
  }

  async readText(path: string, encoding: BufferEncoding = "utf8"): Promise<string> {
    return this.nodeClient().readText(path, encoding);
  }

  async writeFile(path: string, data: Buffer | Uint8Array | string, options: WriteFileOptions = {}): Promise<void> {
    return this.nodeClient().writeFile(path, data, options);
  }

  async writeText(path: string, data: string, options: WriteFileOptions = {}): Promise<void> {
    return this.nodeClient().writeText(path, data, options);
  }

  async mkdir(path: string, options: MkdirOptions = {}): Promise<void> {
    return this.nodeClient().mkdir(path, options);
  }

  async remove(path: string, options: RemoveOptions = {}): Promise<void> {
    return this.nodeClient().remove(path, options);
  }

  async copy(srcPath: string, dstPath: string, options: CopyOptions = {}): Promise<void> {
    return this.nodeClient().copy(srcPath, dstPath, options);
  }

  async move(srcPath: string, dstPath: string, options: MoveOptions = {}): Promise<void> {
    return this.nodeClient().move(srcPath, dstPath, options);
  }

  async chmod(path: string, mode: number, options: ChmodOptions = {}): Promise<void> {
    return this.nodeClient().chmod(path, mode, options);
  }

  async touch(path: string, options: TouchOptions = {}): Promise<void> {
    return this.nodeClient().touch(path, options);
  }

  async uploadDir(localPath: string, remotePath: string, options: UploadDirOptions = {}): Promise<void> {
    await this.nodeClient().uploadArchive(
      remotePath,
      () => directoryArchiveChunks(localPath, options.chunkSize),
      {
        createParents: options.createParents ?? true,
        overwrite: options.overwrite ?? true,
        rpcTimeoutMs: options.rpcTimeoutMs,
      },
    );
  }

  async downloadDir(remotePath: string, localPath: string, options: DownloadDirOptions = {}): Promise<void> {
    await extractDirectoryArchive(
      this.nodeClient().downloadArchive(remotePath, { rpcTimeoutMs: options.rpcTimeoutMs }),
      localPath,
      options.overwrite ?? true,
    );
  }

  private nodeClient(): AllocationClient {
    if (this.currentState === undefined) {
      throw new SandboxStateError("sandbox is not started");
    }
    return this.client.allocation(this.currentState.allocationId);
  }

}
