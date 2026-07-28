/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { imageProcessSpec } from "../src/node/exec.js";
import { workspaceMount } from "../src/types.js";

test("image process spec defaults to workspace mount when mounts are omitted", () => {
  const spec = imageProcessSpec("ghcr.io/cofy-x/agent:latest", ["tool", "run"], {});
  assert.equal(spec.image, "ghcr.io/cofy-x/agent:latest");
  assert.deepEqual(spec.argv, ["tool", "run"]);
  assert.deepEqual(spec.mounts, [
    {
      sandbox_path: "/workspace",
      target_path: "/workspace",
      readonly: false,
      options: [],
    },
  ]);
});

test("image process spec preserves empty and explicit mounts", () => {
  const isolated = imageProcessSpec("agent", ["tool"], { mounts: [] });
  assert.deepEqual(isolated.mounts, []);

  const mounted = imageProcessSpec("agent", ["tool"], {
    mounts: [{ ...workspaceMount("/workspace"), readonly: true, options: ["rshared"] }],
  });
  assert.deepEqual(mounted.mounts, [
    {
      sandbox_path: "/workspace",
      target_path: "/workspace",
      readonly: true,
      options: ["rshared"],
    },
  ]);
});
