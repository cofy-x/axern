/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

export { AxernClient } from "./client/index.js";
export type {
  AxernClientOptions,
  CreateEnvironmentOptions,
  CreateServiceOptions,
} from "./client/index.js";
export { loadAxernContext, loadAxernEnv } from "./config/index.js";
export type { AxernConfig } from "./config/index.js";
export {
  AxernError,
  AxernRpcError,
  isNotFound,
  isCancelled,
  isUnavailable,
  errorRetryable,
  isPermissionDenied,
  isTimeout,
  rpcCode,
  SandboxExecError,
  SandboxStateError,
  SandboxTimeoutError,
  SandboxValidationError,
} from "./errors/index.js";
export { NodeSandboxClient } from "./node/client.js";
export type { NodeSandboxClientOptions } from "./node/client.js";
export { SandboxProcess } from "./node/process.js";
export type { ResourceQuantity } from "./resources.js";
export { Sandbox } from "./sandbox/index.js";
export type { SandboxMetadata, SandboxOptions, SandboxState } from "./sandbox/index.js";
export type {
  ChmodOptions,
  CapabilityDependencyStatus,
  CapabilityProviderStatus,
  CapabilityProviderSummary,
  CapabilityStatus,
  Command,
  ComputerUseDependencyStatus,
  ComputerUseDisplay,
  ComputerUseKeyboardOptions,
  ComputerUseMouseOptions,
  ComputerUseRegion,
  ComputerUseScreenshot,
  ComputerUseScreenshotOptions,
  ComputerUseStatus,
  CopyOptions,
  DownloadArchiveOptions,
  DownloadDirOptions,
  ExecOptions,
  ExecResult,
  ImageExecOptions,
  ImageProcessMount,
  ImageProcessOptions,
  MkdirOptions,
  MoveOptions,
  NodeCallOptions,
  ProcessEvent,
  ProcessOptions,
  ProcessResult,
  RemoveOptions,
  SandboxFileInfo,
  SandboxFileKind,
  TouchOptions,
  TunnelConnectorOptions,
  TunnelMetadata,
  TunnelOptions,
  UploadArchiveOptions,
  UploadDirOptions,
  VolumeMount,
  WriteFileOptions,
} from "./types.js";
export { workspaceMount } from "./types.js";
export { AXERN_VERSION, platformName } from "./version.js";
