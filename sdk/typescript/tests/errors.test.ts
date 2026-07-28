/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  AxernRpcError,
  isNotFound,
  isCancelled,
  isUnavailable,
  errorRetryable,
  isPermissionDenied,
  isTimeout,
  rpcCode,
  SandboxTimeoutError,
} from "../src/errors/index.js";

test("rpc helpers inspect direct and wrapped grpc codes", () => {
  const notFound = new AxernRpcError("read", { code: 5, details: "missing" });
  const denied = new AxernRpcError("exec", { code: "16", details: "auth" });
  const deadline = new AxernRpcError("exec", { code: 4, details: "timeout" });

  assert.equal(rpcCode(notFound), 5);
  assert.equal(isNotFound(notFound), true);
  assert.equal(isPermissionDenied(denied), true);
  assert.equal(isTimeout(deadline), true);
  assert.equal(isTimeout(new SandboxTimeoutError("ready timeout")), true);
  assert.equal(isCancelled(new AxernRpcError("exec", { code: 1 })), true);
  assert.equal(isUnavailable(new AxernRpcError("exec", { code: 14 })), true);
  assert.equal(errorRetryable(deadline), true);
  assert.equal(errorRetryable(notFound), false);
});
