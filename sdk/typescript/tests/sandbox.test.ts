/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { AxernClient } from "../src/client/index.js";
import { SandboxTimeoutError, SandboxValidationError } from "../src/errors/index.js";
import { Sandbox } from "../src/sandbox/index.js";
import { waitRunningRun } from "../src/sandbox/lifecycle.js";
import { NetworkPolicy } from "../src/network-policy.js";

test("sandbox creates image-backed environment and delegates exec", async () => {
  const calls: string[] = [];
  let runOptions: Record<string, unknown> | undefined;
  const fakeClient = {
    async createEnvironment(options: Record<string, unknown>) {
      calls.push(`environment:${String(options.image)}`);
      return { id: "env-1" };
    },
    async createRun(options: Record<string, unknown>) {
      runOptions = options;
      calls.push(`run:${String(options.environmentId)}`);
      return { id: "run-1" };
    },
    async *watchRun() {
      yield { id: "run-1", allocation_id: "alloc-1", status: 3 };
    },
    allocation(allocationId: string) {
      return {
        async exec(command: string) {
          calls.push(`exec:${allocationId}:${command}`);
          return {
            exitCode: 0,
            stdout: Buffer.from("ok"),
            stderr: Buffer.alloc(0),
            stdoutTruncated: false,
            stderrTruncated: false,
            stdoutText: () => "ok",
            stderrText: () => "",
          };
        },
        async uploadArchive(remotePath: string, chunks: () => AsyncIterable<Buffer>) {
          calls.push(`upload:${allocationId}:${remotePath}`);
          for await (const _chunk of chunks()) {
            // Drain the archive stream to verify the factory is usable.
          }
        },
      };
    },
    async cancelRun() {},
    async deleteEnvironment() {},
    close() {},
  } as unknown as AxernClient;

  const networkPolicy = NetworkPolicy.allowDomains("example.com");
  const sandbox = new Sandbox({
    client: fakeClient,
    image: "python:3.12-slim",
    networkPolicy,
    requestCpu: 1,
    requestMemory: 512,
    limitCpu: "1500m",
    limitMemory: "1GiB",
    extensionCapabilities: [{ name: "example.com/accelerator", value: "v1" }],
    declaredOutputs: [{ path: "/tmp/result.json", format: "file", mediaType: "application/json" }],
  });
  await sandbox.start();
  const result = await sandbox.exec("echo ok");
  assert.equal(sandbox.metadata.source, "image");
  assert.equal(sandbox.metadata.environmentId, "env-1");
  await sandbox.close();

  assert.deepEqual(calls, [
    "environment:python:3.12-slim",
    "run:env-1",
    "exec:alloc-1:echo ok",
  ]);
  assert.equal(result.stdoutText(), "ok");
  assert.equal(runOptions?.requestCpu, 1);
  assert.equal(runOptions?.requestMemory, 512);
  assert.equal(runOptions?.limitCpu, "1500m");
  assert.equal(runOptions?.limitMemory, "1GiB");
  assert.deepEqual(runOptions?.extensionCapabilities, [{ name: "example.com/accelerator", value: "v1" }]);
  assert.deepEqual(runOptions?.declaredOutputs, [{ path: "/tmp/result.json", format: "file", mediaType: "application/json" }]);
  assert.equal(runOptions?.networkPolicy, networkPolicy);
});

test("client rejects negative run resource values before RPC", async () => {
  const client = Object.create(AxernClient.prototype) as AxernClient;

  for (const options of [
    { environmentId: "env-1", requestCpu: "-1" },
    { environmentId: "env-1", requestMemory: "-1" },
    { environmentId: "env-1", limitCpu: "-1" },
    { environmentId: "env-1", limitMemory: "-1" },
  ]) {
    await assert.rejects(
      () => client.createRun(options),
      SandboxValidationError,
    );
  }
});

test("sandbox readiness timeout cancels a silent run watch", async () => {
  let watchClosed = false;
  const silentWatch = (signal: AbortSignal): AsyncIterable<Record<string, unknown>> => ({
    [Symbol.asyncIterator]() {
      return {
        next: () => new Promise<IteratorResult<Record<string, unknown>>>((resolve) => {
          signal.addEventListener("abort", () => {
            watchClosed = true;
            resolve({ done: true, value: undefined });
          }, { once: true });
        }),
      };
    },
  });

  await assert.rejects(
    () => waitRunningRun("run-silent", 10, (_runId, signal) => silentWatch(signal)),
    (error: unknown) => error instanceof SandboxTimeoutError && error.message.includes("no state observed"),
  );
  assert.equal(watchClosed, true);
});

test("sandbox readiness rejects every terminal run status", async () => {
  for (const status of [4, 5, 6]) {
    await assert.rejects(
      () => waitRunningRun("run-terminal", 1000, async function* () {
        yield { id: "run-terminal", status, message: "terminal" };
      }),
      (error: unknown) => error instanceof Error && error.message.includes(`became ${status}`),
    );
  }
});
