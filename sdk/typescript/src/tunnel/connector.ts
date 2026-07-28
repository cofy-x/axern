/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import type * as grpc from "@grpc/grpc-js";

import { serviceConstructor } from "../generated/proto.js";
import type { TunnelConnectorOptions } from "../types.js";
import { debugFrame } from "./debug.js";
import { isTerminalRelayError, relayChannelOptions, relayCredentials } from "./relay.js";
import type { GatewayTransportOptions } from "./relay.js";
import { TunnelRelaySession } from "./session.js";
import { parseTcpTarget } from "./target.js";
import type { TunnelSession } from "./types.js";

const defaultPingIntervalMs = 15_000;
const defaultDialTimeoutMs = 5_000;
const defaultMaxStreams = 128;
const initialReconnectBackoffMs = 1_000;
const maxReconnectBackoffMs = 10_000;

export interface TunnelConnectorConfig {
  session: TunnelSession;
  clientToken: string;
  upstream: string;
  transport?: GatewayTransportOptions;
  connector?: TunnelConnectorOptions;
}

export class TunnelConnector {
  private stopped = false;
  private activeCall?: grpc.ClientDuplexStream<Record<string, unknown>, Record<string, unknown>>;
  private activeClient?: grpc.Client;
  private activeSession?: TunnelRelaySession;
  private reconnecting = false;
  private relayTarget = "";
  private upstreamTarget?: ReturnType<typeof parseTcpTarget>;

  constructor(private readonly config: TunnelConnectorConfig) {}

  async start(): Promise<void> {
    const target = this.config.session.client_edge_target || this.config.session.edge_target;
    if (target === undefined || target === "") {
      throw new Error(`tunnel session ${this.config.session.session_id ?? ""} has no relay target`);
    }
    this.upstreamTarget = parseTcpTarget(this.config.upstream);
    this.relayTarget = target;
    this.heartbeat();
    await this.connectOnce();
  }

  async stop(): Promise<void> {
    if (this.stopped) {
      return;
    }
    this.stopped = true;
    this.closeActiveRelay();
  }

  private async connectOnce(): Promise<void> {
    const TunnelRelay = serviceConstructor(["axern", "tunnel", "v1", "TunnelRelay"]);
    const client = new TunnelRelay(this.relayTarget, relayCredentials(this.config.transport), relayChannelOptions(this.config.transport));
    const call = (client as unknown as { ConnectPeer(): grpc.ClientDuplexStream<Record<string, unknown>, Record<string, unknown>> })
      .ConnectPeer();
    const session = new TunnelRelaySession(call, {
      upstreamTarget: this.requiredUpstreamTarget(),
      dialTimeoutMs: this.config.connector?.dialTimeoutMs ?? defaultDialTimeoutMs,
      maxStreams: this.config.connector?.maxStreams ?? defaultMaxStreams,
    });
    this.activeClient = client;
    this.activeCall = call;
    this.activeSession = session;
    call.on("data", (frame: Record<string, unknown>) => {
      debugFrame("recv", frame);
      session.handleFrame(frame);
    });
    call.on("error", (error: grpc.ServiceError) => this.handleRelayClosed(error));
    call.on("end", () => this.handleRelayClosed());
    session.send({
      peer_open: {
        session_id: this.config.session.session_id ?? "",
        peer_kind: 1,
        token: this.config.clientToken,
      },
    });
  }

  private handleRelayClosed(error?: grpc.ServiceError): void {
    if (this.stopped || this.reconnecting) {
      return;
    }
    this.closeActiveRelay();
    if (error !== undefined && isTerminalRelayError(error)) {
      this.stopped = true;
      return;
    }
    this.reconnecting = true;
    void this.reconnectLoop();
  }

  private async reconnectLoop(): Promise<void> {
    let backoffMs = initialReconnectBackoffMs;
    while (!this.stopped) {
      await sleep(backoffMs);
      if (this.stopped) {
        break;
      }
      try {
        await this.connectOnce();
        this.reconnecting = false;
        return;
      } catch (error) {
        if (isTerminalRelayError(error)) {
          this.stopped = true;
          break;
        }
      }
      backoffMs = Math.min(maxReconnectBackoffMs, backoffMs * 2);
    }
    this.reconnecting = false;
  }

  private heartbeat(): void {
    const interval = this.config.connector?.pingIntervalMs ?? defaultPingIntervalMs;
    if (interval <= 0) {
      return;
    }
    const timer = setInterval(() => {
      if (this.stopped) {
        clearInterval(timer);
        return;
      }
      this.activeSession?.send({ ping: { id: String(Date.now()) } });
    }, interval);
    timer.unref();
  }

  private closeActiveRelay(): void {
    const call = this.activeCall;
    const client = this.activeClient;
    const session = this.activeSession;
    this.activeCall = undefined;
    this.activeClient = undefined;
    this.activeSession = undefined;
    call?.removeAllListeners();
    session?.closeAll();
    call?.end();
    client?.close();
  }

  private requiredUpstreamTarget(): ReturnType<typeof parseTcpTarget> {
    if (this.upstreamTarget === undefined) {
      throw new Error("tunnel upstream target is not initialized");
    }
    return this.upstreamTarget;
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
