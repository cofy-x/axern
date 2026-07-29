/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { AxernClient, Sandbox } from "../index.js";
import { loadLocalComposeEnv, sandboxSource } from "./env.js";

loadLocalComposeEnv();

const source = sandboxSource();
const namespace = process.env.AXERN_TS_SMOKE_NAMESPACE ?? "typescript-sdk-smoke";
const memory = `${process.env.AXERN_TS_SMOKE_MEMORY_MB ?? "512"}MiB`;
const client = AxernClient.fromEnv();

const sandbox = await new Sandbox({
  client,
  ...source,
  namespace,
  requestMemory: memory,
  limitMemory: memory,
}).start();

try {
  const capabilities = await sandbox.capabilityStatus();
  if (!capabilities.ready) {
    throw new Error("sandbox capability status is not ready");
  }

  const exec = await sandbox.exec("python -c \"print('ts-sdk-ok')\"", { check: true });
  const execOutput = exec.stdoutText().trim();
  if (execOutput !== "ts-sdk-ok") {
    throw new Error(`unexpected exec output: ${execOutput}`);
  }

  await sandbox.writeText("/tmp/axern-ts/message.txt", "file-ok\n", { createParents: true });
  const fileOutput = (await sandbox.readText("/tmp/axern-ts/message.txt")).trim();
  if (fileOutput !== "file-ok") {
    throw new Error(`unexpected file output: ${fileOutput}`);
  }

  const sandboxProcess = await sandbox.process("python -c \"print('process-ok')\"");
  let processOutput = "";
  for await (const event of sandboxProcess.events()) {
    if (event.kind === "stdout") {
      processOutput += event.data.toString("utf8");
    }
  }
  const processResult = await sandboxProcess.wait();
  if (processResult.exitCode !== 0 || processOutput.trim() !== "process-ok") {
    throw new Error(`unexpected process result: exit=${processResult.exitCode} output=${processOutput.trim()}`);
  }

  const localRoot = await mkdtemp(path.join(os.tmpdir(), "axern-ts-smoke-src-"));
  const downloadRoot = await mkdtemp(path.join(os.tmpdir(), "axern-ts-smoke-dst-"));
  try {
    await mkdir(path.join(localRoot, "nested"), { recursive: true });
    await writeFile(path.join(localRoot, "nested", "archive.txt"), "archive-ok\n");
    await sandbox.uploadDir(localRoot, "/tmp/axern-ts/archive", { overwrite: true });
    await sandbox.downloadDir("/tmp/axern-ts/archive", downloadRoot, { overwrite: true });
    const archiveOutput = await readFile(path.join(downloadRoot, "nested", "archive.txt"), "utf8");
    if (archiveOutput.trim() !== "archive-ok") {
      throw new Error(`unexpected archive output: ${archiveOutput.trim()}`);
    }
  } finally {
    await rm(localRoot, { recursive: true, force: true });
    await rm(downloadRoot, { recursive: true, force: true });
  }

  process.stdout.write("typescript_sdk_local_smoke_ok=true\n");
} finally {
  await sandbox.close();
  client.close();
}
