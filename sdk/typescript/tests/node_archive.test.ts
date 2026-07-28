/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import { Readable } from "node:stream";
import test from "node:test";

import { downloadArchive, uploadArchive } from "../src/node/archive.js";
import type { NodeClientContext } from "../src/node/context.js";

test("uploadArchive sends archive open fields before chunks", async () => {
  const writes: Record<string, unknown>[] = [];
  const client = {
    UploadArchive(callback: (error: Error | null) => void) {
      return new FakeUploadCall(writes, callback);
    },
  };
  const ctx = fakeContext({
    withClient: client,
  });

  await uploadArchive(
    ctx,
    "/remote",
    async function* () {
      yield Buffer.from("tar-bytes");
    },
    { createParents: false, overwrite: false },
  );

  assert.deepEqual(writes[0], {
    open: {
      allocation_id: "alloc-1",
      path: "/remote",
      format: 1,
      create_parents: false,
      overwrite: false,
      symlink_policy: 1,
    },
  });
  assert.deepEqual(writes[1], { chunk: Buffer.from("tar-bytes") });
});

test("downloadArchive sends gateway allocation request and closes client", async () => {
  let closed = 0;
  const ctx = fakeContext({
    rpcClient: () => {
      return {
        DownloadArchive(request: Record<string, unknown>) {
          assert.equal(request.allocation_id, "alloc-1");
          assert.equal(request.path, "/remote");
          assert.equal(request.format, 1);
          assert.equal(request.symlink_policy, 1);
          return Readable.from([{ chunk: Buffer.from("ok") }]);
        },
        close() {
          closed += 1;
        },
      };
    },
  });

  const chunks = [];
  for await (const chunk of downloadArchive(ctx, "/remote")) {
    chunks.push(chunk.toString("utf8"));
  }

  assert.deepEqual(chunks, ["ok"]);
  assert.equal(closed, 1);
});

class FakeUploadCall {
  constructor(
    private readonly writes: Record<string, unknown>[],
    private readonly callback: (error: Error | null) => void,
  ) {}

  write(value: Record<string, unknown>, callback?: (error?: Error | null) => void): boolean {
    this.writes.push(value);
    callback?.(null);
    return true;
  }

  once(): this {
    return this;
  }

  off(): this {
    return this;
  }

  end(): void {
    this.callback(null);
  }

  destroy(error?: Error): void {
    this.callback(error ?? null);
  }
}

function fakeContext(options: {
  withClient?: unknown;
  rpcClient?: () => unknown;
}): NodeClientContext {
  return {
    allocationId: "alloc-1",
    authRequest(payload: Record<string, unknown>) {
      return {
        allocation_id: "alloc-1",
        ...payload,
      };
    },
    async withAuthRetry(_ttl: number, operation: (client: unknown) => Promise<unknown>) {
      return operation(options.withClient);
    },
    rpcClient() {
      return options.rpcClient?.() ?? options.withClient;
    },
  } as unknown as NodeClientContext;
}
