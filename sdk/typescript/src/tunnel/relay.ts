/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import * as grpc from "@grpc/grpc-js";
import { readFileSync } from "node:fs";

export interface GatewayTransportOptions {
  insecure?: boolean;
  tlsCaCert?: string;
  tlsCert?: string;
  tlsKey?: string;
  serverName?: string;
  proxyMode?: "env" | "direct";
}

export function relayCredentials(options: GatewayTransportOptions | undefined): grpc.ChannelCredentials {
  if (options?.insecure === true) {
    return grpc.credentials.createInsecure();
  }
  const rootCerts = options?.tlsCaCert === undefined || options.tlsCaCert === ""
    ? undefined
    : readFileSync(options.tlsCaCert);
  const privateKey = options?.tlsKey === undefined || options.tlsKey === ""
    ? undefined
    : readFileSync(options.tlsKey);
  const certChain = options?.tlsCert === undefined || options.tlsCert === ""
    ? undefined
    : readFileSync(options.tlsCert);
  return grpc.credentials.createSsl(rootCerts, privateKey, certChain);
}

export function relayChannelOptions(options: GatewayTransportOptions | undefined): grpc.ChannelOptions {
  const channelOptions: grpc.ChannelOptions = {};
  if (options?.serverName !== undefined && options.serverName !== "") {
    channelOptions["grpc.ssl_target_name_override"] = options.serverName;
  }
  if (options?.proxyMode === "direct") {
    channelOptions["grpc.enable_http_proxy"] = 0;
  }
  return channelOptions;
}

export function isTerminalRelayError(error: unknown): boolean {
  if (typeof error !== "object" || error === null || !("code" in error)) {
    return false;
  }
  const code = Number((error as { code?: unknown }).code);
  return code === grpc.status.PERMISSION_DENIED ||
    code === grpc.status.UNAUTHENTICATED ||
    code === grpc.status.NOT_FOUND;
}
