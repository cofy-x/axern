/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { mapRpcError } from "../errors/index.js";
import type { DownloadArchiveOptions, UploadArchiveOptions } from "../types.js";
import { nonEmptyPath } from "../validation.js";
import type { NodeClientContext } from "./context.js";
import { downloadArchiveStream, uploadArchiveStream } from "./streams.js";

export async function uploadArchive(
  ctx: NodeClientContext,
  path: string,
  chunks: () => AsyncIterable<Buffer | Uint8Array>,
  options: UploadArchiveOptions = {},
): Promise<void> {
  try {
    await ctx.withAuthRetry(options.leaseTtlSeconds ?? 300, (client) =>
      uploadArchiveStream(client, ctx.authRequest({
        path: nonEmptyPath(path),
        format: 1,
        create_parents: options.createParents ?? true,
        overwrite: options.overwrite ?? true,
        symlink_policy: 1,
      }), chunks(), options.rpcTimeoutMs),
    );
  } catch (error) {
    throw mapRpcError(error, "sandbox upload archive", ctx.allocationId);
  }
}

export async function* downloadArchive(
  ctx: NodeClientContext,
  path: string,
  options: DownloadArchiveOptions = {},
): AsyncIterable<Buffer> {
  const client = ctx.rpcClient();
  try {
    const chunks = downloadArchiveStream(client, ctx.authRequest({
      path: nonEmptyPath(path),
      format: 1,
      symlink_policy: 1,
    }), options.rpcTimeoutMs);
    for await (const chunk of chunks) {
      yield chunk;
    }
  } catch (error) {
    throw mapRpcError(error, "sandbox download archive", ctx.allocationId);
  } finally {
    client.close();
  }
}
