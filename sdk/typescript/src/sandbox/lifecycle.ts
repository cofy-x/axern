/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { SandboxTimeoutError, SandboxValidationError } from "../errors/index.js";
import type { SandboxMetadata, SandboxOptions, SandboxState } from "./index.js";

export const defaultSandboxArgv = ["/bin/sh", "-lc", "sleep infinity"];

export function validateSandboxOptions(options: SandboxOptions): void {
  const sourceCount = [options.templateId, options.image, options.environmentId].filter(
    (source) => source !== undefined && source !== "",
  ).length;
  if (sourceCount !== 1) {
    throw new SandboxValidationError("exactly one of templateId, image, or environmentId is required");
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
  watchRun: (runId: string) => AsyncIterable<Record<string, unknown>>,
): Promise<Record<string, unknown>> {
  const deadline = Date.now() + timeoutMs;
  let lastRun: Record<string, unknown> | undefined;
  for await (const run of watchRun(runId)) {
    lastRun = run;
    const status = Number(run.status ?? 0);
    if (status === 4 && String(run.allocation_id ?? "") !== "") {
      return run;
    }
    if (status === 5 || status === 6 || status === 7) {
      throw new Error(`run ${runId} became ${String(run.status ?? "")} before its sandbox allocation was running: ${String(run.message ?? "")}`);
    }
    if (Date.now() >= deadline) break;
  }
  const details = lastRun === undefined ? "no state observed" : `${String(lastRun.status ?? "")}: ${String(lastRun.message ?? "")}`;
  throw new SandboxTimeoutError(`run ${runId} did not reach a running sandbox allocation: ${details}`);
}

function sandboxSource(options: SandboxOptions): SandboxMetadata["source"] {
  if (options.templateId !== undefined && options.templateId !== "") {
    return "template";
  }
  if (options.image !== undefined && options.image !== "") {
    return "image";
  }
  return "environment";
}
