/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import type * as grpc from "@grpc/grpc-js";
import net from "node:net";

import { debugFrame } from "./debug.js";

const defaultDialTimeoutMs = 5_000;
const defaultMaxStreams = 128;

export interface TunnelRelaySessionOptions {
  upstreamTarget: net.NetConnectOpts;
  dialTimeoutMs?: number;
  maxStreams?: number;
}

export class TunnelRelaySession {
  private readonly streams = new Map<string, net.Socket>();

  constructor(
    private readonly call: grpc.ClientDuplexStream<Record<string, unknown>, Record<string, unknown>>,
    private readonly options: TunnelRelaySessionOptions,
  ) {}

  handleFrame(frame: Record<string, unknown>): void {
    if ("ping" in frame) {
      const ping = frame.ping as Record<string, unknown>;
      this.send({ pong: { id: String(ping.id ?? "") } });
      return;
    }
    if ("stream_open" in frame) {
      this.openLocal(streamIdOf(frame.stream_open));
      return;
    }
    if ("stream_data" in frame) {
      const data = frame.stream_data as Record<string, unknown>;
      this.writeLocal(streamIdOf(data), Buffer.from((data.data as Buffer | Uint8Array | undefined) ?? []));
      return;
    }
    if ("stream_close" in frame) {
      this.closeLocal(streamIdOf(frame.stream_close));
    }
  }

  send(frame: Record<string, unknown>): void {
    debugFrame("send", frame);
    this.call.write(frame);
  }

  closeAll(): void {
    for (const socket of this.streams.values()) {
      socket.destroy();
    }
    this.streams.clear();
  }

  private openLocal(streamId: string): void {
    if (this.streams.size >= (this.options.maxStreams ?? defaultMaxStreams)) {
      this.sendClose(streamId, "max local streams reached");
      return;
    }
    const socket = net.connect(this.options.upstreamTarget);
    const timeout = this.options.dialTimeoutMs ?? defaultDialTimeoutMs;
    this.streams.set(streamId, socket);
    socket.setTimeout(timeout, () => {
      this.sendClose(streamId, `dial timeout after ${timeout}ms`);
      this.closeLocal(streamId);
    });
    socket.on("connect", () => {
      socket.setTimeout(0);
    });
    socket.on("data", (data) => this.send({ stream_data: { stream_id: streamId, data } }));
    socket.on("error", (error) => {
      this.sendClose(streamId, error.message);
      this.closeLocal(streamId);
    });
    socket.on("close", () => {
      this.sendClose(streamId, "");
      this.streams.delete(streamId);
    });
  }

  private writeLocal(streamId: string, data: Buffer): void {
    const socket = this.streams.get(streamId);
    if (socket === undefined) {
      this.sendClose(streamId, "local stream is not open");
      return;
    }
    socket.write(data);
  }

  private closeLocal(streamId: string): void {
    const socket = this.streams.get(streamId);
    this.streams.delete(streamId);
    socket?.destroy();
  }

  private sendClose(streamId: string, message: string): void {
    this.send({ stream_close: { stream_id: streamId, error: message } });
  }
}

function streamIdOf(value: unknown): string {
  const streamId = String((value as Record<string, unknown> | undefined)?.stream_id ?? "");
  if (streamId === "") {
    throw new Error("tunnel stream frame is missing stream_id");
  }
  return streamId;
}
