/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import * as grpc from "@grpc/grpc-js";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import type { Writable } from "node:stream";

import { loadAxernContext, loadAxernEnv, normalizeProxyMode } from "../config/index.js";
import { SandboxStateError, SandboxTimeoutError, mapRpcError } from "../errors/index.js";
import { serviceConstructor, unary } from "../generated/proto.js";
import { AllocationClient } from "../node/client.js";
import { buildResourceSpec } from "../resources.js";
import type { ResourceQuantity } from "../resources.js";
import type { NetworkPolicy } from "../network-policy.js";
import { TunnelControlClient } from "../tunnel/control.js";
import type { GatewayTransportOptions } from "../tunnel/relay.js";
import { required } from "../validation.js";

export interface AxernClientOptions {
  endpoint: string;
  credentials?: grpc.ChannelCredentials;
  tlsCaCert?: string;
  tlsCert?: string;
  tlsKey?: string;
  tlsServerName?: string;
  proxyMode?: "env" | "direct";
}

export interface CreateEnvironmentOptions {
  namespace?: string;
  templateId?: string;
  image?: string;
  registryCredentialId?: string;
  rootfsReadonly?: boolean;
  labels?: Record<string, string>;
}

export interface CreateRunOptions {
  namespace?: string;
  environmentId: string;
  argv?: string[];
  env?: Record<string, string>;
  cwd?: string;
  networkPolicy?: NetworkPolicy;
  extensionCapabilities?: readonly ExtensionCapability[];
  requestCpu?: ResourceQuantity;
  requestMemory?: ResourceQuantity;
  requestEphemeralStorage?: ResourceQuantity;
  limitCpu?: ResourceQuantity;
  limitMemory?: ResourceQuantity;
  limitEphemeralStorage?: ResourceQuantity;
  declaredOutputs?: readonly DeclaredOutput[];
  labels?: Record<string, string>;
}

export type DeclaredOutputFormat = "file" | "tar";

export interface DeclaredOutput {
  path: string;
  format: DeclaredOutputFormat;
  mediaType?: string;
}

export interface SealedOutput {
  outputId: string;
  path: string;
  sizeBytes: number;
  sha256: string;
  mediaType: string;
  format?: DeclaredOutputFormat;
  status: "available" | "missing" | "rejected" | "capture_failed" | "node_unavailable" | "unspecified";
  reason: string;
  sealedAt?: Record<string, unknown>;
  expiresAt?: Record<string, unknown>;
}

export interface ListOptions {
  namespace?: string;
  labels?: Record<string, string>;
  cursor?: string;
  pageSize?: number;
}

export interface ListResult<T> {
  items: T[];
  nextCursor: string;
}

export interface ExtensionCapability {
  name: string;
  value?: string;
}

export interface ReadRunOutputOptions {
  cursor?: string;
  follow?: boolean;
}

export interface WatchRunOptions {
  afterVersion?: number;
  signal?: AbortSignal;
}

export class AxernClient {
  readonly endpoint: string;

  private readonly credentials: grpc.ChannelCredentials;
  private readonly controlOptions: grpc.ChannelOptions;
  private readonly environmentControl: grpc.Client;
  private readonly runControl: grpc.Client;
  private readonly tunnelControl: grpc.Client;
  private readonly gatewayTransport: GatewayTransportOptions;

  constructor(options: AxernClientOptions) {
    this.endpoint = required("endpoint", options.endpoint);
    const proxyMode = normalizeProxyMode(options.proxyMode);
    const tlsConfigured = tlsOptionsConfigured({
      tlsCaCert: options.tlsCaCert,
      tlsCert: options.tlsCert,
      tlsKey: options.tlsKey,
    });
    const tlsServerName = options.tlsServerName ?? defaultLocalTlsServerName(this.endpoint, tlsConfigured);
    this.credentials = options.credentials ?? controlCredentials({
      tlsCaCert: options.tlsCaCert,
      tlsCert: options.tlsCert,
      tlsKey: options.tlsKey,
    });
    this.controlOptions = tlsServerName === undefined || tlsServerName === ""
      ? {}
      : { "grpc.ssl_target_name_override": tlsServerName };
    if (proxyMode === "direct") {
      this.controlOptions["grpc.enable_http_proxy"] = 0;
    }
    this.gatewayTransport = {
      insecure: !tlsConfigured,
      tlsCaCert: options.tlsCaCert,
      tlsCert: options.tlsCert,
      tlsKey: options.tlsKey,
      serverName: tlsServerName,
      proxyMode,
    };

    const EnvironmentControl = serviceConstructor([
      "axern",
      "control",
      "environment",
      "v1",
      "EnvironmentControl",
    ]);
    const RunControl = serviceConstructor(["axern", "control", "run", "v1", "RunControl"]);
    const TunnelControl = serviceConstructor(["axern", "control", "tunnel", "v1", "TunnelControl"]);
    this.environmentControl = new EnvironmentControl(this.endpoint, this.credentials, this.controlOptions);
    this.runControl = new RunControl(this.endpoint, this.credentials, this.controlOptions);
    this.tunnelControl = new TunnelControl(this.endpoint, this.credentials, this.controlOptions);
  }

  static fromEnv(overrides: Partial<AxernClientOptions> = {}): AxernClient {
    const environment = loadAxernEnv();
    return new AxernClient({ ...environment, ...overrides, endpoint: overrides.endpoint ?? environment.endpoint });
  }

  static fromContext(path: string, name = ""): AxernClient {
    const config = loadAxernContext(path, name);
    return new AxernClient({
      endpoint: config.endpoint,
      tlsCaCert: config.tlsCaCert,
      tlsCert: config.tlsCert,
      tlsKey: config.tlsKey,
      tlsServerName: config.tlsServerName,
      proxyMode: config.proxyMode,
    });
  }

  close(): void {
    this.environmentControl.close();
    this.runControl.close();
    this.tunnelControl.close();
  }

  async createEnvironment(options: CreateEnvironmentOptions): Promise<Record<string, unknown>> {
    const namespace = options.namespace ?? "default";
    const sources = [options.templateId, options.image].filter((value) => value !== undefined && value !== "");
    if (sources.length !== 1) {
      throw new Error("exactly one of templateId or image is required");
    }
    const spec: Record<string, unknown> = { namespace };
    if (options.templateId !== undefined && options.templateId !== "") {
      spec.template_id = options.templateId;
    } else {
      spec.image = {
        ref: required("image", options.image),
        registry_credential_id: options.registryCredentialId ?? "",
        rootfs_readonly: options.rootfsReadonly ?? false,
      };
    }
    try {
      const response = await unary<Record<string, unknown>, { environment: Record<string, unknown> }>(
        this.environmentControl,
        "CreateEnvironment",
        { spec, labels: options.labels ?? {} },
      );
      return response.environment;
    } catch (error) {
      throw mapRpcError(error, "create environment");
    }
  }

  async deleteEnvironment(environmentId: string): Promise<void> {
    try {
      await unary(this.environmentControl, "DeleteEnvironment", { environment_id: required("environmentId", environmentId) });
    } catch (error) {
      throw mapRpcError(error, "delete environment");
    }
  }

  async getEnvironment(environmentId: string): Promise<Record<string, unknown>> {
    try {
      const response = await unary<Record<string, unknown>, { environment: Record<string, unknown> }>(
        this.environmentControl,
        "GetEnvironment",
        { environment_id: required("environmentId", environmentId) },
      );
      return response.environment;
    } catch (error) {
      throw mapRpcError(error, "get environment");
    }
  }

  async listEnvironments(options: ListOptions = {}): Promise<ListResult<Record<string, unknown>>> {
    try {
      const response = await unary<Record<string, unknown>, { environments?: Record<string, unknown>[]; next_cursor?: string }>(
        this.environmentControl,
        "ListEnvironments",
        { filter: { namespace: options.namespace ?? "", labels: options.labels ?? {}, cursor: options.cursor ?? "", page_size: options.pageSize ?? 0 } },
      );
      return { items: response.environments ?? [], nextCursor: response.next_cursor ?? "" };
    } catch (error) {
      throw mapRpcError(error, "list environments");
    }
  }

  async createRun(options: CreateRunOptions): Promise<Record<string, unknown>> {
    const resources = buildResourceSpec(options);
    try {
      const response = await unary<Record<string, unknown>, { run: Record<string, unknown> }>(
        this.runControl,
        "CreateRun",
        {
          namespace: options.namespace ?? "default",
          environment_id: required("environmentId", options.environmentId),
          config: {
            argv: options.argv ?? [],
            env: options.env ?? {},
            cwd: options.cwd ?? "",
            ...(options.networkPolicy === undefined
              ? {}
              : { network: { egress_policy: options.networkPolicy.toWire() } }),
            extension_capability_requirements: (options.extensionCapabilities ?? []).map((capability) => ({
              capability: { name: capability.name, value: capability.value ?? "" },
            })),
            declared_outputs: (options.declaredOutputs ?? []).map((output) => ({
              path: output.path,
              format: output.format === "file" ? 1 : 2,
              media_type: output.mediaType ?? "",
            })),
            resources,
          },
          labels: options.labels ?? {},
        },
      );
      return response.run;
    } catch (error) {
      throw mapRpcError(error, "create run");
    }
  }

  async getRun(runId: string): Promise<Record<string, unknown>> {
    try {
      const response = await unary<Record<string, unknown>, { run: Record<string, unknown> }>(
        this.runControl,
        "GetRun",
        { run_id: required("runId", runId) },
      );
      return response.run;
    } catch (error) {
      throw mapRpcError(error, "get run");
    }
  }

  async listRuns(options: ListOptions & { statuses?: number[] } = {}): Promise<ListResult<Record<string, unknown>>> {
    try {
      const response = await unary<Record<string, unknown>, { runs?: Record<string, unknown>[]; next_cursor?: string }>(
        this.runControl,
        "ListRuns",
        { filter: { namespace: options.namespace ?? "", labels: options.labels ?? {}, statuses: options.statuses ?? [], cursor: options.cursor ?? "", page_size: options.pageSize ?? 0 } },
      );
      return { items: response.runs ?? [], nextCursor: response.next_cursor ?? "" };
    } catch (error) {
      throw mapRpcError(error, "list runs");
    }
  }

  async waitRun(runId: string, timeoutMs?: number): Promise<Record<string, unknown>> {
    const initial = await this.getRun(runId);
    if (terminalRun(initial)) return initial;
    const controller = new AbortController();
    const timer = timeoutMs === undefined ? undefined : setTimeout(() => controller.abort(), timeoutMs);
    try {
      for await (const run of this.watchRun(runId, { afterVersion: Number(initial.version ?? 0), signal: controller.signal })) {
        if (terminalRun(run)) return run;
      }
    } finally {
      if (timer !== undefined) clearTimeout(timer);
    }
    if (controller.signal.aborted) throw new SandboxTimeoutError(`run ${runId} wait timed out`);
    throw new SandboxStateError(`run ${runId} watch ended before a terminal state`);
  }

  async cancelRun(runId: string): Promise<void> {
    try {
      await unary(this.runControl, "CancelRun", { run_id: required("runId", runId) });
    } catch (error) {
      throw mapRpcError(error, "cancel run");
    }
  }

  async *watchRun(runId: string, options: WatchRunOptions = {}): AsyncGenerator<Record<string, unknown>> {
    const afterVersion = options.afterVersion ?? 0;
    const signal = options.signal;
    if (afterVersion < 0) {
      throw new Error("afterVersion must be non-negative");
    }
    let version = afterVersion;
    let retryDelayMs = 100;
    for (;;) {
      if (signal?.aborted) return;
      const stream = serverStream(this.runControl, "WatchRun", {
        run_id: required("runId", runId),
        after_version: version,
      });
      const cancel = () => stream.cancel();
      signal?.addEventListener("abort", cancel, { once: true });
      try {
        for await (const response of stream) {
          const run = response.run as Record<string, unknown> | undefined;
          if (run === undefined) continue;
          const nextVersion = Number(run.version ?? 0);
          if (nextVersion <= version) continue;
          version = nextVersion;
          retryDelayMs = 100;
          yield run;
        }
        return;
      } catch (error) {
        if (signal?.aborted) return;
        if (!transientReadError(error)) throw mapRpcError(error, "watch run");
      } finally {
        signal?.removeEventListener("abort", cancel);
      }
      await sleep(retryDelayMs, signal);
      retryDelayMs = Math.min(retryDelayMs * 2, 2_000);
    }
  }

  async *readRunOutput(runId: string, options: ReadRunOutputOptions = {}): AsyncGenerator<Record<string, unknown>> {
    // Output is Allocation-local and may be unavailable after cleanup; callers
    // that need durable bytes must consume and persist them before then.
    const response = await unary<Record<string, unknown>, { run?: Record<string, unknown> }>(
      this.runControl,
      "GetRun",
      { run_id: required("runId", runId) },
    );
    const allocationId = String(response.run?.allocation_id ?? "");
    if (allocationId === "") throw new Error(`run ${runId} output is not available yet`);
    let cursor = options.cursor ?? "";
    let retryDelayMs = 100;
    let notFoundSince = 0;
    for (;;) {
      const NodeSandbox = serviceConstructor(["axern", "node", "sandbox", "v1", "NodeSandbox"]);
      const node = new NodeSandbox(this.endpoint, this.credentials, this.controlOptions);
      const stream = serverStream(node, "ReadOutput", {
        allocation_id: allocationId,
        cursor,
        follow: options.follow ?? false,
      });
      try {
        for await (const event of stream) {
          cursor = String(event.next_cursor ?? cursor);
          retryDelayMs = 100;
          notFoundSince = 0;
          yield event;
        }
        return;
      } catch (error) {
        const code = (error as { code?: number }).code;
        const startupNotFound = code === grpc.status.NOT_FOUND;
        if (!(options.follow ?? false) || (!transientReadError(error) && !startupNotFound)) {
          throw mapRpcError(error, "read run output");
        }
        if (startupNotFound) {
          notFoundSince ||= Date.now();
          if (Date.now() - notFoundSince >= 30_000) throw mapRpcError(error, "read run output");
        }
      } finally {
        node.close();
      }
      await sleep(retryDelayMs);
      retryDelayMs = Math.min(retryDelayMs * 2, 2_000);
    }
  }

  async getSealedOutputManifest(runId: string): Promise<SealedOutput[]> {
    const run = await this.getRun(runId);
    const allocationId = String(run.allocation_id ?? "");
    if (allocationId === "") throw new Error(`run ${runId} has no allocation`);
    const NodeSandbox = serviceConstructor(["axern", "node", "sandbox", "v1", "NodeSandbox"]);
    const node = new NodeSandbox(this.endpoint, this.credentials, this.controlOptions);
    try {
      const response = await unary<Record<string, unknown>, { outputs?: Record<string, unknown>[] }>(
        node,
        "GetSealedOutputManifest",
        { allocation_id: allocationId },
      );
      return (response.outputs ?? []).map(sealedOutput);
    } catch (error) {
      throw mapRpcError(error, "get sealed output manifest", allocationId);
    } finally {
      node.close();
    }
  }

  async downloadSealedOutput(runId: string, outputId: string, destination: Writable): Promise<SealedOutput> {
    const outputs = await this.getSealedOutputManifest(runId);
    const selected = outputs.find((output) => output.outputId === outputId);
    if (selected === undefined || selected.status !== "available") {
      throw new Error(`sealed output ${outputId} is not available`);
    }
    const run = await this.getRun(runId);
    const allocationId = String(run.allocation_id ?? "");
    const NodeSandbox = serviceConstructor(["axern", "node", "sandbox", "v1", "NodeSandbox"]);
    const node = new NodeSandbox(this.endpoint, this.credentials, this.controlOptions);
    const stream = serverStream(node, "DownloadSealedOutput", { allocation_id: allocationId, output_id: outputId, offset: "0" });
    const digest = createHash("sha256");
    let offset = 0;
    try {
      for await (const response of stream) {
        const data = Buffer.from((response.data as Buffer | Uint8Array | undefined) ?? []);
        const nextOffset = Number(response.next_offset ?? offset + data.length);
        if (nextOffset !== offset + data.length) throw new Error("sealed output returned a non-contiguous offset");
        digest.update(data);
        await writeChunk(destination, data);
        offset = nextOffset;
      }
    } catch (error) {
      if (typeof error === "object" && error !== null && "code" in error) {
        throw mapRpcError(error, "download sealed output", allocationId);
      }
      throw error;
    } finally {
      node.close();
    }
    if (offset !== selected.sizeBytes) throw new Error("sealed output size does not match its manifest");
    if (digest.digest("hex") !== selected.sha256) throw new Error("sealed output digest does not match its manifest");
    return selected;
  }

  allocation(allocationId: string): AllocationClient {
    return new AllocationClient({
      allocationId: required("allocationId", allocationId),
      target: this.endpoint,
      credentials: this.credentials,
      channelOptions: this.controlOptions,
    });
  }

  tunnelClient(): TunnelControlClient {
    return new TunnelControlClient(this.tunnelControl);
  }

  tunnelTransport(): GatewayTransportOptions {
    return { ...this.gatewayTransport };
  }
}

function serverStream(client: grpc.Client, method: string, request: Record<string, unknown>): grpc.ClientReadableStream<Record<string, unknown>> {
  const fn = (client as unknown as Record<string, Function>)[method];
  return fn.call(client, request) as grpc.ClientReadableStream<Record<string, unknown>>;
}

function transientReadError(error: unknown): boolean {
  const code = (error as { code?: number }).code;
  return code === grpc.status.UNAVAILABLE || code === grpc.status.DEADLINE_EXCEEDED;
}

function terminalRun(run: Record<string, unknown>): boolean {
  const status = Number(run.status ?? 0);
  return status === 4 || status === 5 || status === 6;
}

function sealedOutput(value: Record<string, unknown>): SealedOutput {
  const formats: Record<number, DeclaredOutputFormat | undefined> = { 1: "file", 2: "tar" };
  const statuses = ["unspecified", "available", "missing", "rejected", "capture_failed", "node_unavailable"] as const;
  return {
    outputId: String(value.output_id ?? ""),
    path: String(value.path ?? ""),
    sizeBytes: Number(value.size_bytes ?? 0),
    sha256: String(value.sha256 ?? ""),
    mediaType: String(value.media_type ?? ""),
    format: formats[Number(value.format ?? 0)],
    status: statuses[Number(value.status ?? 0)] ?? "unspecified",
    reason: String(value.reason ?? ""),
    sealedAt: value.sealed_at as Record<string, unknown> | undefined,
    expiresAt: value.expires_at as Record<string, unknown> | undefined,
  };
}

function writeChunk(destination: Writable, chunk: Buffer): Promise<void> {
  if (destination.write(chunk)) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      destination.off("drain", drained);
      destination.off("error", failed);
    };
    const drained = () => { cleanup(); resolve(); };
    const failed = (error: Error) => { cleanup(); reject(error); };
    destination.once("drain", drained);
    destination.once("error", failed);
  });
}

function sleep(milliseconds: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    const complete = () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", complete);
      resolve();
    };
    const timer = setTimeout(complete, milliseconds);
    signal?.addEventListener("abort", complete, { once: true });
  });
}

function controlCredentials(options: {
  tlsCaCert?: string;
  tlsCert?: string;
  tlsKey?: string;
}): grpc.ChannelCredentials {
  const configured = [options.tlsCaCert, options.tlsCert, options.tlsKey].filter((value) => value !== undefined && value !== "");
  if (configured.length === 0) {
    return grpc.credentials.createInsecure();
  }
  if (configured.length !== 3) {
    throw new Error("mTLS requires tlsCaCert, tlsCert, and tlsKey");
  }
  const rootCerts = readFileSync(options.tlsCaCert as string);
  const certChain = readFileSync(options.tlsCert as string);
  const privateKey = readFileSync(options.tlsKey as string);
  return grpc.credentials.createSsl(rootCerts, privateKey, certChain);
}

function tlsOptionsConfigured(options: {
  tlsCaCert?: string;
  tlsCert?: string;
  tlsKey?: string;
}): boolean {
  return [options.tlsCaCert, options.tlsCert, options.tlsKey].some((value) => value !== undefined && value !== "");
}

function defaultLocalTlsServerName(target: string, tlsConfigured: boolean): string | undefined {
  if (!tlsConfigured) {
    return undefined;
  }
  const host = targetHost(target);
  return host === "127.0.0.1" || host === "::1" ? "localhost" : undefined;
}

function targetHost(target: string): string {
  const withoutScheme = target.replace(/^[a-z][a-z0-9+.-]*:\/\//i, "");
  if (withoutScheme.startsWith("[")) {
    const end = withoutScheme.indexOf("]");
    return end === -1 ? withoutScheme : withoutScheme.slice(1, end);
  }
  return withoutScheme.split(":", 1)[0] ?? withoutScheme;
}
