/** Explicit environment and context loaders for the Axern SDK. */

import { readFileSync } from "node:fs";

export interface AxernConfig {
  endpoint: string;
  serviceUrl?: string;
  sshEndpoint?: string;
  sshIdentityFile?: string;
  tlsCaCert?: string;
  tlsCert?: string;
  tlsKey?: string;
  tlsServerName?: string;
  proxyMode: "env" | "direct";
}

export function loadAxernEnv(overrides: Partial<AxernConfig> = {}): AxernConfig {
  return {
    endpoint: overrides.endpoint ?? process.env.AXERN_ENDPOINT ?? "127.0.0.1:25000",
    serviceUrl: overrides.serviceUrl ?? process.env.AXERN_SERVICE_URL,
    sshEndpoint: overrides.sshEndpoint ?? process.env.AXERN_SSH_ENDPOINT,
    sshIdentityFile: overrides.sshIdentityFile ?? process.env.AXERN_SSH_IDENTITY_FILE,
    tlsCaCert: overrides.tlsCaCert ?? process.env.AXERN_TLS_CA_CERT,
    tlsCert: overrides.tlsCert ?? process.env.AXERN_TLS_CERT,
    tlsKey: overrides.tlsKey ?? process.env.AXERN_TLS_KEY,
    tlsServerName: overrides.tlsServerName ?? process.env.AXERN_TLS_SERVER_NAME,
    proxyMode: normalizeProxyMode(overrides.proxyMode ?? process.env.AXERN_PROXY_MODE),
  };
}

export function loadAxernContext(path: string, name = ""): AxernConfig {
  const file = JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
  rejectUnknown(file, ["current_context", "contexts", "agent_profiles"], "config");
  const contextName = name || stringValue(file.current_context);
  if (!contextName) throw new Error("Axern context name is required");
  const contexts = objectValue(file.contexts, "contexts");
  const context = objectValue(contexts[contextName], `context ${JSON.stringify(contextName)}`);
  rejectUnknown(context, ["endpoint", "service_url", "ssh_endpoint", "ssh_identity_file", "tls", "proxy_mode"], "context");
  const tls = objectValue(context.tls, "context.tls");
  rejectUnknown(tls, ["ca_cert", "cert", "key", "server_name"], "context.tls");
  const config: AxernConfig = {
    endpoint: stringValue(context.endpoint),
    serviceUrl: stringValue(context.service_url) || undefined,
    sshEndpoint: stringValue(context.ssh_endpoint) || undefined,
    sshIdentityFile: stringValue(context.ssh_identity_file) || undefined,
    tlsCaCert: stringValue(tls.ca_cert),
    tlsCert: stringValue(tls.cert),
    tlsKey: stringValue(tls.key),
    tlsServerName: stringValue(tls.server_name) || undefined,
    proxyMode: normalizeProxyMode(stringValue(context.proxy_mode)),
  };
  if (!config.endpoint || !config.tlsCaCert || !config.tlsCert || !config.tlsKey) {
    throw new Error("context requires endpoint and tls.ca_cert, tls.cert, and tls.key");
  }
  return config;
}

export function normalizeProxyMode(value: string | undefined): "env" | "direct" {
  const mode = value?.trim() || "env";
  if (mode !== "env" && mode !== "direct") throw new Error("proxy_mode must be 'env' or 'direct'");
  return mode;
}

function objectValue(value: unknown, path: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error(`${path} must be an object`);
  return value as Record<string, unknown>;
}

function stringValue(value: unknown): string {
  if (value === undefined) return "";
  if (typeof value !== "string") throw new Error("context string field has invalid type");
  return value.trim();
}

function rejectUnknown(value: Record<string, unknown>, allowed: string[], path: string): void {
  const unknown = Object.keys(value).filter((key) => !allowed.includes(key)).sort();
  if (unknown.length > 0) throw new Error(`${path} contains unknown field ${JSON.stringify(unknown[0])}`);
}
