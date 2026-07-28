/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { directoryArchiveChunks, extractDirectoryArchive } from "../src/sandbox/archive.js";

test("directory archive round-trips nested files", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-src-"));
  const target = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-dst-"));
  await mkdir(path.join(root, "nested"), { recursive: true });
  await writeFile(path.join(root, "nested", "message.txt"), "hello archive\n");

  await extractDirectoryArchive(directoryArchiveChunks(root), target);

  assert.equal(await readFile(path.join(target, "nested", "message.txt"), "utf8"), "hello archive\n");
});

test("safe extraction accepts a tar root directory entry", async () => {
  const target = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-root-"));

  await extractDirectoryArchive(tarArchive([
    { name: ".", type: "directory" },
    { name: "message.txt", type: "file", body: "root archive\n" },
  ]), target);

  assert.equal(await readFile(path.join(target, "message.txt"), "utf8"), "root archive\n");
});

test("safe extraction rejects a non-directory tar root entry", async () => {
  const target = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-root-file-"));

  await assert.rejects(
    () => extractDirectoryArchive(tarArchive([{ name: ".", type: "file", body: "x" }]), target),
    /root entry .* must be a directory/,
  );
});

test("directory archive upload rejects local symlinks", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-link-"));
  await writeFile(path.join(root, "file.txt"), "ok");
  await symlink(path.join(root, "file.txt"), path.join(root, "link.txt"));

  await assert.rejects(
    async () => {
      for await (const _chunk of directoryArchiveChunks(root)) {
        // Drain the archive stream to surface producer errors.
      }
    },
    /symlink/,
  );
});

test("safe extraction rejects escaping archive entries", async () => {
  const target = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-safe-"));
  const archive = tarLikeEscapingArchive();

  await assert.rejects(
    () => extractDirectoryArchive(archive, target),
    /escapes target directory/,
  );
});

test("safe extraction rejects symlink targets", async () => {
  const outside = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-outside-"));
  const parent = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-parent-"));
  const link = path.join(parent, "link");
  const fixture = await fixtureDirectory();
  await symlink(outside, link);

  await assert.rejects(
    () => extractDirectoryArchive(directoryArchiveChunks(fixture), link),
    /symlink/,
  );
});

async function* tarLikeEscapingArchive(): AsyncIterable<Buffer> {
  const { pack } = await import("tar-stream");
  const stream = pack();
  stream.entry({ name: "../escape.txt", type: "file", size: 1 }, Buffer.from("x"));
  stream.finalize();
  for await (const chunk of stream) {
    yield Buffer.from(chunk);
  }
}

async function* tarArchive(entries: Array<{
  name: string;
  type: "directory" | "file";
  body?: string;
}>): AsyncIterable<Buffer> {
  const { pack } = await import("tar-stream");
  const stream = pack();
  for (const entry of entries) {
    const body = Buffer.from(entry.body ?? "");
    stream.entry(
      { name: entry.name, type: entry.type, size: entry.type === "file" ? body.length : undefined },
      entry.type === "file" ? body : undefined,
    );
  }
  stream.finalize();
  for await (const chunk of stream) {
    yield Buffer.from(chunk);
  }
}

async function fixtureDirectory(): Promise<string> {
  const root = await mkdtemp(path.join(os.tmpdir(), "axern-ts-archive-fixture-"));
  await writeFile(path.join(root, "file.txt"), "ok");
  return root;
}
