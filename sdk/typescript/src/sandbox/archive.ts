/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, opendir, readdir, lstat } from "node:fs/promises";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

import * as tar from "tar-stream";

export const defaultArchiveChunkSize = 1024 * 1024;

export async function* directoryArchiveChunks(
  localPath: string,
  chunkSize = defaultArchiveChunkSize,
): AsyncIterable<Buffer> {
  const pack = tar.pack();
  const producer = writeDirectoryArchive(pack, localPath);
  producer.catch((error: unknown) => pack.destroy(error instanceof Error ? error : new Error(String(error))));
  try {
    for await (const chunk of pack) {
      yield* splitChunk(Buffer.from(chunk), chunkSize);
    }
    await producer;
  } catch (error) {
    await producer.catch(() => undefined);
    pack.destroy(error instanceof Error ? error : new Error(String(error)));
    throw error;
  }
}

export async function extractDirectoryArchive(
  chunks: AsyncIterable<Buffer | Uint8Array>,
  localPath: string,
  overwrite = true,
): Promise<void> {
  if (!overwrite && !(await localPathMissingOrEmpty(localPath))) {
    throw new Error(`local path ${localPath} already exists and is not empty`);
  }
  await ensureNoLocalSymlink(localPath);
  const rootAbs = path.resolve(localPath);
  await mkdir(rootAbs, { recursive: true, mode: 0o755 });
  const extract = tar.extract();
  const input = Readable.from(chunks);
  let entryError: unknown;

  extract.on("entry", (header, stream, next) => {
    void (async () => {
      try {
        const target = safeTarTarget(rootAbs, header.name);
        if (target === rootAbs && header.type !== "directory") {
          throw new Error(`archive root entry ${header.name} must be a directory`);
        }
        if (header.type === "symlink" || header.type === "link") {
          throw new Error(`archive entry ${header.name} uses unsupported link type`);
        }
        if (header.type === "directory") {
          await ensureNoLocalSymlinkPath(rootAbs, target);
          await mkdir(target, { recursive: true, mode: (header.mode ?? 0o755) & 0o777 });
          stream.resume();
          next();
          return;
        }
        if (header.type !== "file" && header.type !== "contiguous-file" && header.type !== undefined && header.type !== null) {
          throw new Error(`archive entry ${header.name} has unsupported type ${header.type}`);
        }
        if (!overwrite) {
          await ensureMissing(target);
        }
        const parent = path.dirname(target);
        await ensureNoLocalSymlinkPath(rootAbs, parent);
        await mkdir(parent, { recursive: true, mode: 0o755 });
        await ensureNoLocalSymlink(target);
        await pipeline(stream, createWriteStream(target, { mode: (header.mode ?? 0o644) & 0o777 }));
        next();
      } catch (error) {
        entryError = error;
        extract.destroy();
        stream.resume();
        next();
      }
    })();
  });

  try {
    await pipeline(input, extract);
  } catch (error) {
    throw entryError ?? error;
  }
}

async function writeDirectoryArchive(pack: tar.Pack, root: string): Promise<void> {
  try {
    const info = await lstat(root);
    if (info.isSymbolicLink()) {
      throw new Error(`local path ${root} is a symlink`);
    }
    if (!info.isDirectory()) {
      throw new Error(`local path ${root} is not a directory`);
    }
    await addDirectoryEntries(pack, root, root);
    pack.finalize();
  } catch (error) {
    pack.destroy(error instanceof Error ? error : new Error(String(error)));
    throw error;
  }
}

async function addDirectoryEntries(pack: tar.Pack, root: string, current: string): Promise<void> {
  const dir = await opendir(current);
  for await (const entry of dir) {
    const fullPath = path.join(current, entry.name);
    const info = await lstat(fullPath);
    if (info.isSymbolicLink()) {
      throw new Error(`local symlink ${fullPath} is not supported`);
    }
    const name = path.relative(root, fullPath).split(path.sep).join("/");
    if (info.isDirectory()) {
      await entryHeader(pack, {
        name: name.endsWith("/") ? name : `${name}/`,
        type: "directory",
        mode: info.mode & 0o777,
        mtime: info.mtime,
      });
      await addDirectoryEntries(pack, root, fullPath);
      continue;
    }
    if (!info.isFile()) {
      throw new Error(`local path ${fullPath} is not a regular file`);
    }
    const tarEntry = pack.entry({
      name,
      type: "file",
      size: info.size,
      mode: info.mode & 0o777,
      mtime: info.mtime,
    });
    await pipeline(createReadStream(fullPath), tarEntry);
  }
}

function entryHeader(pack: tar.Pack, header: tar.Headers): Promise<void> {
  return new Promise((resolve, reject) => {
    pack.entry(header, (error) => {
      if (error) {
        reject(error);
      } else {
        resolve();
      }
    });
  });
}

function* splitChunk(chunk: Buffer, chunkSize: number): Iterable<Buffer> {
  const size = chunkSize > 0 ? chunkSize : defaultArchiveChunkSize;
  for (let offset = 0; offset < chunk.length; offset += size) {
    yield chunk.subarray(offset, Math.min(offset + size, chunk.length));
  }
}

function safeTarTarget(rootAbs: string, name: string): string {
  if (name.length === 0) {
    throw new Error("archive entry path is empty");
  }
  const clean = path.posix.normalize(name);
  const rootEntry = clean === "." && (name === "." || name === "./");
  if (path.posix.isAbsolute(name) || (clean === "." && !rootEntry) || clean === ".." || clean.startsWith("../")) {
    throw new Error(`archive entry ${name} escapes target directory`);
  }
  if (rootEntry) {
    return rootAbs;
  }
  const target = path.resolve(rootAbs, ...clean.split("/"));
  const rel = path.relative(rootAbs, target);
  if (rel === ".." || rel.startsWith(`..${path.sep}`) || path.isAbsolute(rel)) {
    throw new Error(`archive entry ${name} escapes target directory`);
  }
  return target;
}

async function ensureMissing(target: string): Promise<void> {
  try {
    await lstat(target);
  } catch (error) {
    if (isNodeError(error, "ENOENT")) {
      return;
    }
    throw error;
  }
  throw new Error(`local path ${target} already exists`);
}

async function ensureNoLocalSymlink(target: string): Promise<void> {
  try {
    const info = await lstat(target);
    if (info.isSymbolicLink()) {
      throw new Error(`local path ${target} is a symlink`);
    }
  } catch (error) {
    if (isNodeError(error, "ENOENT")) {
      return;
    }
    throw error;
  }
}

async function ensureNoLocalSymlinkPath(rootAbs: string, targetAbs: string): Promise<void> {
  const rel = path.relative(rootAbs, targetAbs);
  if (rel === "") {
    await ensureNoLocalSymlink(rootAbs);
    return;
  }
  let current = rootAbs;
  for (const part of rel.split(path.sep)) {
    current = path.join(current, part);
    await ensureNoLocalSymlink(current);
  }
}

async function localPathMissingOrEmpty(localPath: string): Promise<boolean> {
  try {
    const info = await lstat(localPath);
    if (info.isSymbolicLink()) {
      throw new Error(`local path ${localPath} is a symlink`);
    }
    if (!info.isDirectory()) {
      return false;
    }
    return (await readdir(localPath)).length === 0;
  } catch (error) {
    if (isNodeError(error, "ENOENT")) {
      return true;
    }
    throw error;
  }
}

function isNodeError(error: unknown, code: string): boolean {
  return typeof error === "object" && error !== null && "code" in error && (error as { code?: unknown }).code === code;
}
