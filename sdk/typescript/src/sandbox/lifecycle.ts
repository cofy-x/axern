/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { SandboxTimeoutError, SandboxValidationError } from "../errors/index.js";
import type { SandboxMetadata, SandboxOptions, SandboxState } from "./index.js";

export const defaultSandboxArgv = ["/bin/sh", "-lc", "sleep infinity"];

const runStatus = {
  running: 3,
  succeeded: 4,
  failed: 5,
  cancelled: 6,
} as const;

export function validateSandboxOptions(options: SandboxOptions): void {
  const sourceCount = [options.image, options.environmentId].filter(
    (source) => source !== undefined && source !== "",
  ).length;
  if (sourceCount !== 1) {
    throw new SandboxValidationError("exactly one of image or environmentId is required");
  }
}

export function sandboxLabels(labels: Record<string, string> | undefined): Record<string, string> {
  return {
    "axern.sdk": "typescript",
    ...(labels ?? {}),
  };
}

export function sandboxMetadata(options: SandboxOptions, state: SandboxState): SandboxMetadata {
  return {
    ...state,
    namespace: options.namespace ?? "default",
    labels: sandboxLabels(options.labels),
    source: sandboxSource(options),
  };
}

export async function waitRunningRun(
  runId: string,
  timeoutMs: number,
  watchRun: (runId: string, signal: AbortSignal) => AsyncIterable<Record<string, unknown>>,
): Promise<Record<string, unknown>> {
  const deadline = Date.now() + timeoutMs;
  let lastRun: Record<string, unknown> | undefined;
  const controller = new AbortController();
  const iterator = watchRun(runId, controller.signal)[Symbol.asyncIterator]();
  try {
    for (;;) {
      const remainingMs = deadline - Date.now();
      if (remainingMs <= 0) break;
      const result = await nextBeforeDeadline(iterator, remainingMs);
      if (result === undefined || result.done) break;
      const run = result.value;
      lastRun = run;
      const status = Number(run.status ?? 0);
      if (status === runStatus.running && String(run.allocation_id ?? "") !== "") {
        return run;
      }
      if (status === runStatus.succeeded || status === runStatus.failed || status === runStatus.cancelled) {
        throw new Error(`run ${runId} became ${String(run.status ?? "")} before its sandbox allocation was running: ${String(run.message ?? "")}`);
      }
    }
  } finally {
    controller.abort();
    try {
      const close = iterator.return?.();
      if (close !== undefined) void close.catch(() => undefined);
    } catch {
      // The readiness result owns the error surface; stream cleanup is best effort.
    }
  }
  const details = lastRun === undefined ? "no state observed" : `${String(lastRun.status ?? "")}: ${String(lastRun.message ?? "")}`;
  throw new SandboxTimeoutError(`run ${runId} did not reach a running sandbox allocation: ${details}`);
}

async function nextBeforeDeadline<T>(
  iterator: AsyncIterator<T>,
  timeoutMs: number,
): Promise<IteratorResult<T> | undefined> {
  let timer: number | undefined;
  try {
    return await Promise.race([
      iterator.next(),
      new Promise<undefined>((resolve) => {
        timer = setTimeout(resolve, timeoutMs);
      }),
    ]);
  } finally {
    if (timer !== undefined) clearTimeout(timer);
  }
}

function sandboxSource(options: SandboxOptions): SandboxMetadata["source"] {
  if (options.image !== undefined && options.image !== "") {
    return "image";
  }
  return "environment";
}
