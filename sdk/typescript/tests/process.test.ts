/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";

import { AxernRpcError } from "../src/errors/index.js";
import { SandboxValidationError } from "../src/errors/index.js";
import { process as startProcess } from "../src/node/attached_process.js";
import type { NodeClientContext } from "../src/node/context.js";
import { SandboxProcess } from "../src/node/process.js";

test("process waitReady does not drop stdout or exit", async () => {
  const call = new FakeProcessCall();
  const process = new SandboxProcess({
    allocationId: "alloc-1",
    call: call.rpcCall(),
    closeClient: () => call.closeClient(),
  });

  call.emit("data", { ready: {} });
  await process.waitReady();
  call.emit("data", { stdout: Buffer.from("hello\n") });
  call.emit("data", { exit: { exit_code: 0, message: "done" } });

  const events = [];
  for await (const event of process.events()) {
    events.push(event);
  }

  assert.equal(events[0]?.kind, "stdout");
  assert.equal(events[0]?.kind === "stdout" ? events[0].data.toString("utf8") : "", "hello\n");
  assert.equal(events[1]?.kind, "exit");
  assert.deepEqual(await process.wait(), { exitCode: 0, message: "done" });
  assert.deepEqual(await process.wait(), { exitCode: 0, message: "done" });
  assert.equal(call.closed, true);
});

test("process wait maps stream errors", async () => {
  const call = new FakeProcessCall();
  const process = new SandboxProcess({
    allocationId: "alloc-2",
    call: call.rpcCall(),
    closeClient: () => call.closeClient(),
  });

  call.emit("error", { code: 13, details: "broken stream" });

  await assert.rejects(
    () => process.wait(),
    (error) => error instanceof AxernRpcError && error.operation === "sandbox process" && error.allocationId === "alloc-2",
  );
});

test("process sends initial terminal size in the atomic open request", async () => {
  const call = new FakeProcessCall();
  const ctx = {
    allocationId: "alloc-1",
    rpcClient: () => ({ Process: () => call.rpcCall(), close: () => undefined }),
    authRequest: (payload: Record<string, unknown>) => ({ allocation_id: "alloc-1", ...payload }),
  } as unknown as NodeClientContext;

  const ready = startProcess(ctx, ["sh"], { tty: true, initialCols: 120, initialRows: 40 });
  queueMicrotask(() => call.emit("data", { ready: {} }));
  const process = await ready;

  assert.deepEqual(call.writes[0], {
    open: {
      allocation_id: "alloc-1",
      spec: {
        argv: ["sh"],
        env: {},
        cwd: "",
        timeout_seconds: 0,
        tty: true,
        user: "",
      },
      initial_size: { cols: 120, rows: 40 },
    },
  });
  await process.close();
});

test("process rejects an incomplete initial terminal size", async () => {
  const ctx = {} as NodeClientContext;
  await assert.rejects(
    () => startProcess(ctx, ["sh"], { initialCols: 80 }),
    (error) => error instanceof SandboxValidationError,
  );
});

class FakeProcessCall extends EventEmitter {
  closed = false;
  writes: unknown[] = [];

  write(value: unknown): boolean {
    this.writes.push(value);
    return true;
  }

  end(): void {
    this.closed = true;
  }

  closeClient(): void {
    this.closed = true;
  }

  rpcCall() {
    return this as unknown as ConstructorParameters<typeof SandboxProcess>[0]["call"];
  }
}
