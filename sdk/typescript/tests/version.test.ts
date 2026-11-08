/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";

import { AXERN_VERSION, platformName } from "../src/index.js";

test("exports the Axern platform name", () => {
  assert.equal(platformName(), "axern");
  assert.equal(AXERN_VERSION, readFileSync(new URL("../../../VERSION", import.meta.url), "utf8").trim());
});
