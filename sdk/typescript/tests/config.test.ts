import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { AxernClient } from "../src/client/index.js";
import { loadAxernContext, loadAxernEnv } from "../src/config/index.js";

test("loads the explicit Axern context schema", () => {
  const dir = mkdtempSync(join(tmpdir(), "axern-context-"));
  const path = join(dir, "config.json");
  writeFileSync(path, JSON.stringify({
    current_context: "hk",
    contexts: {
      hk: {
        endpoint: "gateway.example:443",
        service_url: "https://services.example",
        ssh_endpoint: "gateway.example:22",
        ssh_identity_file: "/keys/hk",
        tls: { ca_cert: "/ca", cert: "/cert", key: "/key", server_name: "gateway.example" },
        proxy_mode: "direct",
      },
    },
  }));

  const context = loadAxernContext(path);
  assert.equal(context.endpoint, "gateway.example:443");
  assert.equal(context.serviceUrl, "https://services.example");
  assert.equal(context.proxyMode, "direct");
});

test("rejects obsolete context fields", () => {
  const dir = mkdtempSync(join(tmpdir(), "axern-context-"));
  const path = join(dir, "config.json");
  writeFileSync(path, JSON.stringify({
    current_context: "old",
    contexts: {
      old: {
        endpoint: "gateway.example:443",
        control_target: "old.example:443",
        tls: { ca_cert: "/ca", cert: "/cert", key: "/key" },
      },
    },
  }));
  assert.throws(() => loadAxernContext(path), /unknown field/);
});

test("environment loader exposes endpoint terminology", () => {
  const config = loadAxernEnv({ endpoint: "override:443", proxyMode: "direct" });
  assert.equal(config.endpoint, "override:443");
  assert.equal(config.proxyMode, "direct");
});

test("rejects unknown top-level config fields", () => {
  const dir = mkdtempSync(join(tmpdir(), "axern-context-"));
  const path = join(dir, "config.json");
  writeFileSync(path, JSON.stringify({ current_context: "", contexts: {}, control_target: "old" }));
  assert.throws(() => loadAxernContext(path), /unknown field/);
});

test("explicit client rejects an invalid proxy mode at runtime", () => {
  assert.throws(
    () => new AxernClient({ endpoint: "gateway.example:443", proxyMode: "tunnel" as "direct" }),
    /proxy_mode/,
  );
});
