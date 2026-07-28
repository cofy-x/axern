/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { cp, mkdir, readdir, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = path.resolve(__dirname, "..");
const sourceRoot = path.resolve(packageRoot, "../proto");
const targetRoot = path.resolve(packageRoot, "dist/proto");

await rm(targetRoot, { recursive: true, force: true });
await copyPublicProtoTree(sourceRoot, targetRoot);

async function copyPublicProtoTree(sourceDir, targetDir) {
  const entries = await readdir(sourceDir, { withFileTypes: true });
  for (const entry of entries) {
    const source = path.join(sourceDir, entry.name);
    const target = path.join(targetDir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "private") {
        continue;
      }
      await copyPublicProtoTree(source, target);
      continue;
    }
    if (!entry.isFile() || !entry.name.endsWith(".proto")) {
      continue;
    }
    await mkdir(path.dirname(target), { recursive: true });
    await cp(source, target);
  }
}
