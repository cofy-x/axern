/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { AXERN_VERSION, platformName } from "../src/index.js";

test("exports the Axern platform name", () => {
  assert.equal(platformName(), "axern");
  assert.equal(AXERN_VERSION, "0.3.0");
});
