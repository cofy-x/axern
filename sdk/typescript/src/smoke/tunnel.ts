/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import http from "node:http";

import { AxernClient, Sandbox } from "../index.js";
import { loadLocalComposeEnv, sandboxSource } from "./env.js";

loadLocalComposeEnv();

const source = sandboxSource();
const namespace = process.env.AXERN_TS_SMOKE_NAMESPACE ?? "typescript-sdk-smoke";
const memory = `${process.env.AXERN_TS_SMOKE_MEMORY_MB ?? "512"}MiB`;
const proxyPort = Number(process.env.AXERN_TS_SMOKE_TUNNEL_PROXY_PORT ?? "8786");
const client = AxernClient.fromEnv();

const server = http.createServer((_request, response) => {
  response.writeHead(200, { "content-type": "text/plain" });
  response.end("tunnel-ok\n");
});

await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));

const address = server.address();
if (address === null || typeof address === "string") {
  throw new Error("failed to allocate local tunnel upstream port");
}

const sandbox = await new Sandbox({
  client,
  ...source,
  namespace,
  requestMemory: memory,
  limitMemory: memory,
  tunnel: {
    upstream: `127.0.0.1:${address.port}`,
    proxyPort,
  },
}).start();

try {
  const tunnel = sandbox.metadata.tunnel;
  if (tunnel === undefined) {
    throw new Error("sandbox metadata did not include tunnel");
  }
  const result = await sandbox.exec([
    "python",
    "-c",
    [
      "import sys, urllib.request",
      `data = urllib.request.urlopen("http://${tunnel.boundAddr}/", timeout=10).read().decode().strip()`,
      "print(data)",
    ].join("; "),
  ]);
  if (result.exitCode !== 0) {
    throw new Error(`tunnel request failed: ${result.stderrText().trim()}`);
  }
  const output = result.stdoutText().trim();
  if (output !== "tunnel-ok") {
    throw new Error(`unexpected tunnel output: ${output}`);
  }
  process.stdout.write(`typescript_sdk_tunnel_smoke_ok=true bound_addr=${tunnel.boundAddr}\n`);
} finally {
  await sandbox.close();
  client.close();
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
