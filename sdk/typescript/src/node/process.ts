/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import type * as grpc from "@grpc/grpc-js";

import { mapRpcError } from "../errors/index.js";
import type { ProcessEvent, ProcessResult } from "../types.js";

export interface SandboxProcessOptions {
  allocationId: string;
  call: grpc.ClientDuplexStream<Record<string, unknown>, Record<string, unknown>>;
  closeClient: () => void;
}

export class SandboxProcess {
  readonly allocationId: string;

  private readonly call: grpc.ClientDuplexStream<Record<string, unknown>, Record<string, unknown>>;
  private readonly closeClient: () => void;
  private readonly queue: ProcessEvent[] = [];
  private readonly waiters: Array<(value: IteratorResult<ProcessEvent>) => void> = [];
  private error: unknown;
  private ended = false;
  private exit?: ProcessResult;
  private paused = false;
  private closed = false;

  private static readonly maxQueuedEvents = 64;
  private static readonly resumeQueuedEvents = 32;

  constructor(options: SandboxProcessOptions) {
    this.allocationId = options.allocationId;
    this.call = options.call;
    this.closeClient = options.closeClient;

    this.call.on("data", (message: Record<string, unknown>) => this.enqueue(processEvent(message)));
    this.call.on("error", (error: unknown) => {
      this.error = error;
      this.finish();
    });
    this.call.on("end", () => this.finish());
  }

  write(data: Buffer | Uint8Array | string): Promise<void> {
    return this.writeRequest({ stdin: Buffer.isBuffer(data) ? data : Buffer.from(data) });
  }

  closeStdin(): Promise<void> {
    return this.writeRequest({ close_stdin: true });
  }

  resize(cols: number, rows: number): Promise<void> {
    return this.writeRequest({ resize: { cols, rows } });
  }

  signal(signal: string): Promise<void> {
    return this.writeRequest({ signal: { signal } });
  }

  terminate(): Promise<void> {
    return this.signal("TERM");
  }

  kill(): Promise<void> {
    return this.signal("KILL");
  }

  async waitReady(): Promise<void> {
    const next = await this.nextEvent();
    if (next.done === true) {
      if (this.error !== undefined) {
        throw this.error;
      }
      throw new Error("sandbox process stream ended before ready");
    }
    if (next.value.kind !== "ready") {
      this.queue.unshift(next.value);
    }
  }

  async wait(): Promise<ProcessResult> {
    if (this.exit !== undefined) {
      return this.exit;
    }
    for await (const event of this.events()) {
      if (event.kind === "exit") {
        return { exitCode: event.exitCode, message: event.message };
      }
    }
    if (this.error !== undefined) {
      throw mapRpcError(this.error, "sandbox process", this.allocationId);
    }
    throw new Error("sandbox process stream ended before exit");
  }

  async close(): Promise<void> {
    if (this.closed) return;
    if (!this.ended && this.exit === undefined) {
      await this.terminate().catch(() => undefined);
    }
    this.closed = true;
    this.call.end();
    this.call.cancel();
    this.closeClient();
  }

  async *events(): AsyncIterable<ProcessEvent> {
    while (true) {
      const next = await this.nextEvent();
      if (next.done === true) {
        if (this.error !== undefined) {
          throw mapRpcError(this.error, "sandbox process", this.allocationId);
        }
        return;
      }
      yield next.value;
      if (next.value.kind === "exit") {
        return;
      }
    }
  }

  private nextEvent(): Promise<IteratorResult<ProcessEvent>> {
    const event = this.queue.shift();
    if (event !== undefined) {
      if (this.paused && this.queue.length <= SandboxProcess.resumeQueuedEvents) {
        this.paused = false;
        this.call.resume();
      }
      return Promise.resolve({ done: false, value: event });
    }
    if (this.ended) {
      return Promise.resolve({ done: true, value: undefined });
    }
    return new Promise((resolve) => this.waiters.push(resolve));
  }

  private enqueue(event: ProcessEvent): void {
    if (event.kind === "exit") {
      this.exit = { exitCode: event.exitCode, message: event.message };
      this.call.end();
      this.closeClient();
    }
    const waiter = this.waiters.shift();
    if (waiter !== undefined) {
      waiter({ done: false, value: event });
    } else {
      if (this.queue.length >= SandboxProcess.maxQueuedEvents) {
        this.error = new Error("sandbox process consumer is too slow; event queue limit exceeded");
        this.call.cancel();
        this.finish();
        return;
      }
      this.queue.push(event);
      if (!this.paused && this.queue.length >= SandboxProcess.maxQueuedEvents) {
        this.paused = true;
        this.call.pause();
      }
    }
  }

  private writeRequest(request: Record<string, unknown>): Promise<void> {
    if (this.closed || this.ended) {
      return Promise.reject(new Error("sandbox process stream is closed"));
    }
    return new Promise((resolve, reject) => {
      this.call.write(request, (error?: Error | null) => {
        if (error !== undefined && error !== null) {
          reject(mapRpcError(error, "sandbox process write", this.allocationId));
          return;
        }
        resolve();
      });
    });
  }

  private finish(): void {
    if (this.ended) {
      return;
    }
    this.ended = true;
    this.closeClient();
    for (const waiter of this.waiters.splice(0)) {
      waiter({ done: true, value: undefined });
    }
  }
}

function processEvent(message: Record<string, unknown>): ProcessEvent {
  if ("ready" in message) {
    return { kind: "ready" };
  }
  if ("stdout" in message) {
    return { kind: "stdout", data: Buffer.from((message.stdout as Buffer | Uint8Array | undefined) ?? []) };
  }
  if ("stderr" in message) {
    return { kind: "stderr", data: Buffer.from((message.stderr as Buffer | Uint8Array | undefined) ?? []) };
  }
  if ("exit" in message) {
    const exit = message.exit as Record<string, unknown> | undefined;
    return {
      kind: "exit",
      exitCode: Number(exit?.exit_code ?? 0),
      message: String(exit?.message ?? ""),
    };
  }
  return { kind: "ready" };
}
