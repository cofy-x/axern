/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";

import { NetworkPolicy, cidrRule, portRange } from "../src/index.js";

test("denyDns normalizes and deduplicates domains", () => {
  assert.deepEqual(NetworkPolicy.denyDns("GitHub.COM.", "github.com", "*.BÜCHER.example").toWire(), {
    dns_deny: { denied_domains: ["github.com", "*.xn--bcher-kva.example"] },
  });
});

test("strict serializes domain and CIDR grants", () => {
  assert.deepEqual(
    NetworkPolicy.strict({
      domains: ["example.com"],
      cidrRules: [cidrRule("192.0.2.0/24", "tcp", portRange(22), portRange(8000, 8002))],
    }).toWire(),
    {
      strict: {
        allowed_domains: ["example.com"],
        allowed_cidrs: [{
          cidr: "192.0.2.0/24",
          protocol: 1,
          ports: [{ start: 22, end: 22 }, { start: 8000, end: 8002 }],
        }],
      },
    },
  );
});

test("denyAll is strict empty and invalid rules fail locally", () => {
  assert.deepEqual(NetworkPolicy.denyAll().toWire(), { strict: { allowed_domains: [], allowed_cidrs: [] } });
  assert.throws(() => NetworkPolicy.allowDomains("https://example.com"));
  assert.throws(() => NetworkPolicy.allowDomains("127.0.0.1"));
  assert.throws(() => portRange(0));
  assert.throws(() => cidrRule("not-a-cidr", "tcp", portRange(22)));
});
