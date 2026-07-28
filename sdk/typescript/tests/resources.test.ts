/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { SandboxValidationError } from "../src/errors/index.js";
import { buildResourceSpec } from "../src/resources.js";

test("resource spec parses friendly quantities", () => {
  assert.deepEqual(
    buildResourceSpec({
      requestCpu: 0.5,
      requestMemory: "128Mi",
      limitCpu: "1500m",
      limitMemory: "1Gi",
    }),
    {
      requests: {
        cpu_milli: 500,
        memory_bytes: 128 * 1024 * 1024,
      },
      limits: {
        cpu_milli: 1500,
        memory_bytes: 1024 * 1024 * 1024,
      },
    },
  );
});

test("resource spec rejects invalid quantities", () => {
  for (const options of [
    { requestCpu: "-1" },
    { requestMemory: "-1" },
    { limitCpu: "-1" },
    { limitMemory: "-1" },
    { requestCpu: "0.5m" },
    { requestMemory: "0.5B" },
  ]) {
    assert.throws(
      () => buildResourceSpec(options),
      SandboxValidationError,
    );
  }
});
