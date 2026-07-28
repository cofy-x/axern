/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { loadAxernPackage } from "../src/generated/proto.js";

test("loads Axern control and node proto services", () => {
  const pkg = loadAxernPackage() as Record<string, unknown>;
  const axern = pkg.axern as Record<string, unknown>;
  const control = axern.control as Record<string, unknown>;
  const environment = control.environment as Record<string, unknown>;
  const gateway = control.gateway as Record<string, unknown>;
  const service = control.service as Record<string, unknown>;
  const node = axern.node as Record<string, unknown>;
  const sandbox = node.sandbox as Record<string, unknown>;

  assert.equal(typeof ((environment.v1 as Record<string, unknown>).EnvironmentControl), "function");
  assert.equal(typeof ((gateway.v1 as Record<string, unknown>).GatewayControl), "function");
  assert.equal(typeof ((service.v1 as Record<string, unknown>).ServiceControl), "function");
  assert.equal(typeof ((sandbox.v1 as Record<string, unknown>).NodeSandbox), "function");
});
