/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import * as grpc from "@grpc/grpc-js";
import { readFileSync } from "node:fs";

import { loadAxernContext, loadAxernEnv, normalizeProxyMode } from "../config/index.js";
import { mapRpcError } from "../errors/index.js";
import { serviceConstructor, unary } from "../generated/proto.js";
import { NodeSandboxClient } from "../node/client.js";
import { buildResourceSpec } from "../resources.js";
import type { ResourceQuantity } from "../resources.js";
import { TunnelControlClient } from "../tunnel/control.js";
import type { VolumeMount } from "../types.js";
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

export interface CreateServiceOptions {
  namespace?: string;
  environmentId: string;
  argv?: string[];
  env?: Record<string, string>;
  cwd?: string;
  runtimeClass?: string;
  volumes?: readonly VolumeMount[];
  requestCpu?: ResourceQuantity;
  requestMemory?: ResourceQuantity;
  requestWritableLayer?: ResourceQuantity;
  limitCpu?: ResourceQuantity;
  limitMemory?: ResourceQuantity;
  limitWritableLayer?: ResourceQuantity;
  labels?: Record<string, string>;
}

export interface ReadRunOutputOptions {
  cursor?: string;
  follow?: boolean;
}

export class AxernClient {
  readonly endpoint: string;

  private readonly credentials: grpc.ChannelCredentials;
  private readonly controlOptions: grpc.ChannelOptions;
  private readonly environmentControl: grpc.Client;
  private readonly runControl: grpc.Client;
  private readonly serviceControl: grpc.Client;
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
    const ServiceControl = serviceConstructor(["axern", "control", "service", "v1", "ServiceControl"]);
    const TunnelControl = serviceConstructor(["axern", "control", "tunnel", "v1", "TunnelControl"]);
    this.environmentControl = new EnvironmentControl(this.endpoint, this.credentials, this.controlOptions);
    this.runControl = new RunControl(this.endpoint, this.credentials, this.controlOptions);
    this.serviceControl = new ServiceControl(this.endpoint, this.credentials, this.controlOptions);
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
    this.serviceControl.close();
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

  async *watchRun(runId: string, afterVersion = 0): AsyncGenerator<Record<string, unknown>> {
    if (afterVersion < 0) {
      throw new Error("afterVersion must be non-negative");
    }
    let version = afterVersion;
    let retryDelayMs = 100;
    for (;;) {
      const stream = serverStream(this.runControl, "WatchRun", {
        run_id: required("runId", runId),
        after_version: version,
      });
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
        if (!transientReadError(error)) throw mapRpcError(error, "watch run");
      }
      await sleep(retryDelayMs);
      retryDelayMs = Math.min(retryDelayMs * 2, 2_000);
    }
  }

  async *readRunOutput(runId: string, options: ReadRunOutputOptions = {}): AsyncGenerator<Record<string, unknown>> {
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

  async createService(options: CreateServiceOptions): Promise<Record<string, unknown>> {
    const resources = buildResourceSpec(options);
    try {
      const response = await unary<Record<string, unknown>, { service: Record<string, unknown> }>(
        this.serviceControl,
        "CreateService",
        {
          namespace: options.namespace ?? "default",
          environment_id: required("environmentId", options.environmentId),
          replicas: 1,
          config: {
            argv: options.argv ?? [],
            env: options.env ?? {},
            cwd: options.cwd ?? "",
            runtime_class: options.runtimeClass ?? "",
            volume_mounts: serviceVolumeMounts(options.volumes),
            resources,
          },
          labels: options.labels ?? {},
        },
      );
      return response.service;
    } catch (error) {
      throw mapRpcError(error, "create service");
    }
  }

  async deleteService(serviceId: string): Promise<void> {
    try {
      await unary(this.serviceControl, "DeleteService", { service_id: required("serviceId", serviceId) });
    } catch (error) {
      throw mapRpcError(error, "delete service");
    }
  }

  async listServiceReplicas(serviceId: string): Promise<Record<string, unknown>[]> {
    try {
      const response = await unary<Record<string, unknown>, { replicas?: Record<string, unknown>[] }>(
        this.serviceControl,
        "ListServiceReplicas",
        {
          service_id: required("serviceId", serviceId),
          filter: { view: 2 },
        },
      );
      return response.replicas ?? [];
    } catch (error) {
      throw mapRpcError(error, "list service replicas");
    }
  }

  nodeSandbox(allocationId: string): NodeSandboxClient {
    return new NodeSandboxClient({
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

function serviceVolumeMounts(mounts: readonly VolumeMount[] | undefined): Record<string, unknown>[] {
  return (mounts ?? []).map((mount) => ({
    name: mount.name,
    target: mount.target,
    readonly: mount.readonly ?? false,
    options: [...(mount.options ?? [])],
  }));
}

function serverStream(client: grpc.Client, method: string, request: Record<string, unknown>): grpc.ClientReadableStream<Record<string, unknown>> {
  const fn = (client as unknown as Record<string, Function>)[method];
  return fn.call(client, request) as grpc.ClientReadableStream<Record<string, unknown>>;
}

function transientReadError(error: unknown): boolean {
  const code = (error as { code?: number }).code;
  return code === grpc.status.UNAVAILABLE || code === grpc.status.DEADLINE_EXCEEDED;
}

function sleep(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
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
