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
    runtimeClass: options.runtimeClass ?? "",
    labels: sandboxLabels(options.labels),
    source: sandboxSource(options),
  };
}

export async function waitReadyReplica(
  serviceId: string,
  timeoutMs: number,
  listReplicas: (serviceId: string) => Promise<Record<string, unknown>[]>,
): Promise<Record<string, unknown>> {
  const deadline = Date.now() + timeoutMs;
  let lastReplicas: Record<string, unknown>[] = [];
  while (Date.now() < deadline) {
    lastReplicas = await listReplicas(serviceId);
    const candidate = lastReplicas.find((replica) =>
      replica.ready === true &&
      replica.ended !== true &&
      replica.outdated !== true &&
      Number(replica.status ?? 0) === 4
    );
    if (candidate !== undefined) {
      return candidate;
    }
    await sleep(2_000);
  }
  const details = lastReplicas.map((replica) => `${String(replica.id ?? "")}:${String(replica.status ?? "")}`).join(", ");
  throw new SandboxTimeoutError(`service ${serviceId} did not produce a ready sandbox replica: ${details}`);
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

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
