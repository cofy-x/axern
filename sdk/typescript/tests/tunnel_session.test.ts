/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import net from "node:net";
import type { AddressInfo } from "node:net";
import test from "node:test";

import { TunnelRelaySession } from "../src/tunnel/session.js";

test("tunnel session preserves uint64 stream ids as strings", async () => {
  const server = net.createServer((socket) => {
    socket.on("data", (data) => socket.write(data));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assertAddressInfo(address);

  const writes: Record<string, unknown>[] = [];
  const call = {
    write(frame: Record<string, unknown>) {
      writes.push(frame);
      return true;
    },
  };
  const streamId = "2695449439778111487";
  const session = new TunnelRelaySession(call as never, {
    upstreamTarget: { host: "127.0.0.1", port: address.port },
  });

  try {
    session.handleFrame({ stream_open: { stream_id: streamId } });
    session.handleFrame({ stream_data: { stream_id: streamId, data: Buffer.from("hello") } });
    await eventually(() => {
      const data = writes.find((frame) => "stream_data" in frame)?.stream_data as Record<string, unknown> | undefined;
      assert.equal(data?.stream_id, streamId);
      assert.equal(Buffer.from((data?.data as Buffer | undefined) ?? []).toString(), "hello");
    });
  } finally {
    session.closeAll();
    await new Promise<void>((resolve, reject) => {
      server.close((error) => {
        if (error === undefined) {
          resolve();
        } else {
          reject(error);
        }
      });
    });
  }
});

async function eventually(assertion: () => void): Promise<void> {
  const deadline = Date.now() + 1_000;
  let lastError: unknown;
  while (Date.now() < deadline) {
    try {
      assertion();
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
  }
  throw lastError;
}

function assertAddressInfo(address: string | AddressInfo | null): asserts address is AddressInfo {
  assert.notEqual(address, null);
  assert.equal(typeof address, "object");
}
