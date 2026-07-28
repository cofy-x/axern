/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { AxernClient, Sandbox } from "../src/index.js";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import http from "node:http";
import os from "node:os";
import path from "node:path";

const upstream = await startLocalUpstream();
const configPath = process.env.AXERN_CONFIG ?? path.join(os.homedir(), ".config", "axern", "config.json");
const client = AxernClient.fromContext(configPath, process.env.AXERN_CONTEXT ?? "");
let sandbox: Sandbox | undefined;

try {
  sandbox = await new Sandbox({
	client,
    image: process.env.AXERN_EXAMPLE_IMAGE ?? "python:3.12-slim",
    namespace: process.env.AXERN_EXAMPLE_NAMESPACE ?? "typescript-sdk-example",
    requestMemory: "512MiB",
    limitMemory: "512MiB",
    tunnel: {
      upstream: upstream.target,
      proxyPort: Number(process.env.AXERN_EXAMPLE_TUNNEL_PROXY_PORT ?? "8786"),
    },
  }).start();

  console.log("metadata", sandbox.metadata);

  const result = await sandbox.exec("python - <<'PY'\nprint('hello from axern typescript')\nPY", {
    check: true,
  });
  process.stdout.write(result.stdoutText());

  const sandboxProcess = await sandbox.process("python - <<'PY'\nprint('streaming process works')\nPY");
  for await (const event of sandboxProcess.events()) {
    if (event.kind === "stdout") {
      process.stdout.write(event.data);
    }
  }

  await sandbox.writeText("/tmp/axern/message.txt", "file api works\n", { createParents: true });
  process.stdout.write(await sandbox.readText("/tmp/axern/message.txt"));

  if (sandbox.metadata.tunnel !== undefined) {
    const tunnelResult = await sandbox.exec(
      `python -c "import urllib.request; print(urllib.request.urlopen('http://${sandbox.metadata.tunnel.boundAddr}/index.txt', timeout=10).read().decode().strip())"`,
      { check: true },
    );
    process.stdout.write(tunnelResult.stdoutText());
  }

  const uploadRoot = await mkdtemp(path.join(os.tmpdir(), "axern-ts-example-upload-"));
  const downloadRoot = await mkdtemp(path.join(os.tmpdir(), "axern-ts-example-download-"));
  try {
    await mkdir(path.join(uploadRoot, "nested"), { recursive: true });
    await writeFile(path.join(uploadRoot, "nested", "archive.txt"), "archive api works\n");
    await sandbox.uploadDir(uploadRoot, "/tmp/axern/archive");
    await sandbox.downloadDir("/tmp/axern/archive", downloadRoot);
    process.stdout.write(await readFile(path.join(downloadRoot, "nested", "archive.txt"), "utf8"));
  } finally {
    await rm(uploadRoot, { recursive: true, force: true });
    await rm(downloadRoot, { recursive: true, force: true });
  }
} finally {
  await sandbox?.close();
  client.close();
  await upstream.stop();
}

async function startLocalUpstream(): Promise<{ target: string; stop(): Promise<void> }> {
  const server = http.createServer((_request, response) => {
    response.writeHead(200, { "content-type": "text/plain" });
    response.end("tunnel api works\n");
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("failed to allocate local upstream port");
  }
  return {
    target: `127.0.0.1:${address.port}`,
    stop() {
      return new Promise<void>((resolve, reject) => {
        server.close((error) => {
          if (error === undefined) {
            resolve();
          } else {
            reject(error);
          }
        });
      });
    },
  };
}
