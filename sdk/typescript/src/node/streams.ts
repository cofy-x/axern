/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import * as grpc from "@grpc/grpc-js";

export function uploadArchiveStream(
  client: grpc.Client,
  open: Record<string, unknown>,
  chunks: AsyncIterable<Buffer | Uint8Array>,
  deadlineMs: number | undefined,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const callback = (error: grpc.ServiceError | null) => {
      if (error) {
        reject(error);
      } else {
        resolve();
      }
    };
    const fn = (client as unknown as { UploadArchive: Function }).UploadArchive;
    const call = deadlineMs === undefined
      ? fn.call(client, callback)
      : fn.call(client, new grpc.Metadata(), { deadline: Date.now() + deadlineMs }, callback);

    void (async () => {
      try {
        await writeStream(call, { open });
        for await (const chunk of chunks) {
          if (chunk.byteLength > 0) {
            await writeStream(call, { chunk: Buffer.from(chunk) });
          }
        }
        call.end();
      } catch (error) {
        call.destroy(error instanceof Error ? error : new Error(String(error)));
        reject(error);
      }
    })();
  });
}

export function downloadArchiveStream(
  client: grpc.Client,
  request: Record<string, unknown>,
  deadlineMs: number | undefined,
): AsyncIterable<Buffer> {
  const fn = (client as unknown as { DownloadArchive: Function }).DownloadArchive;
  const call = deadlineMs === undefined
    ? fn.call(client, request)
    : fn.call(client, request, new grpc.Metadata(), { deadline: Date.now() + deadlineMs });
  return streamChunks(call);
}

async function* streamChunks(call: NodeJS.ReadableStream): AsyncIterable<Buffer> {
  for await (const message of call as AsyncIterable<Record<string, unknown>>) {
    const chunk = message.chunk as Buffer | Uint8Array | undefined;
    if (chunk !== undefined && chunk.byteLength > 0) {
      yield Buffer.from(chunk);
    }
  }
}

function writeStream(
  stream: {
    write(value: unknown, callback?: (error?: Error | null) => void): boolean;
    once(event: "error", listener: (error: Error) => void): unknown;
    off(event: "error", listener: (error: Error) => void): unknown;
  },
  value: unknown,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const onError = (error: Error) => {
      stream.off("error", onError);
      reject(error);
    };
    stream.once("error", onError);
    stream.write(value, (error?: Error | null) => {
      stream.off("error", onError);
      if (error) {
        reject(error);
      } else {
        resolve();
      }
    });
  });
}
