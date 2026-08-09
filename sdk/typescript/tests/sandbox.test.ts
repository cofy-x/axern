/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { AxernClient } from "../src/client/index.js";
import { SandboxValidationError } from "../src/errors/index.js";
import { Sandbox } from "../src/sandbox/index.js";

test("sandbox creates image-backed environment and delegates exec", async () => {
  const calls: string[] = [];
  let serviceOptions: Record<string, unknown> | undefined;
  const fakeClient = {
    async createEnvironment(options: Record<string, unknown>) {
      calls.push(`environment:${String(options.image)}`);
      return { id: "env-1" };
    },
    async createService(options: Record<string, unknown>) {
      serviceOptions = options;
      calls.push(`service:${String(options.environmentId)}`);
      return { id: "svc-1" };
    },
    async listServiceReplicas() {
      return [{ id: "alloc-1", node_id: "node-1", attempt: 1, ready: true, status: 4 }];
    },
    nodeSandbox(allocationId: string) {
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
    async deleteService() {},
    async purgeService() {},
    async deleteEnvironment() {},
    close() {},
  } as unknown as AxernClient;

  const sandbox = new Sandbox({
    client: fakeClient,
    image: "python:3.12-slim",
    requestCpu: 1,
    requestMemory: 512,
    limitCpu: "1500m",
    limitMemory: "1GiB",
    extensionCapabilities: [{ name: "example.com/accelerator", value: "v1" }],
    volumes: [{ name: "workspace", target: "/workspace", readonly: true, options: ["rbind"] }],
  });
  await sandbox.start();
  const result = await sandbox.exec("echo ok");
  assert.equal(sandbox.metadata.source, "image");
  assert.equal(sandbox.metadata.environmentId, "env-1");
  await sandbox.close();

  assert.deepEqual(calls, [
    "environment:python:3.12-slim",
    "service:env-1",
    "exec:alloc-1:echo ok",
  ]);
  assert.equal(result.stdoutText(), "ok");
  assert.equal(serviceOptions?.requestCpu, 1);
  assert.equal(serviceOptions?.requestMemory, 512);
  assert.equal(serviceOptions?.limitCpu, "1500m");
  assert.equal(serviceOptions?.limitMemory, "1GiB");
  assert.deepEqual(serviceOptions?.extensionCapabilities, [{ name: "example.com/accelerator", value: "v1" }]);
  assert.deepEqual(serviceOptions?.volumes, [{ name: "workspace", target: "/workspace", readonly: true, options: ["rbind"] }]);
});

test("client rejects negative service resource values before RPC", async () => {
  const client = Object.create(AxernClient.prototype) as AxernClient;

  for (const options of [
    { environmentId: "env-1", requestCpu: "-1" },
    { environmentId: "env-1", requestMemory: "-1" },
    { environmentId: "env-1", limitCpu: "-1" },
    { environmentId: "env-1", limitMemory: "-1" },
  ]) {
    await assert.rejects(
      () => client.createService(options),
      SandboxValidationError,
    );
  }
});
