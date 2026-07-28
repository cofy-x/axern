/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { SandboxValidationError } from "./errors/index.js";
import type { Command } from "./types.js";

export function required(name: string, value: string | undefined | null): string {
  if (value === undefined || value === null || value.length === 0) {
    throw new SandboxValidationError(`${name} is required`);
  }
  return value;
}

export function nonEmptyPath(path: string): string {
  if (path.length === 0) {
    throw new SandboxValidationError("path is required");
  }
  return path;
}

export function normalizeCommand(command: Command): string[] {
  if (typeof command === "string") {
    if (command.length === 0) {
      throw new SandboxValidationError("command is required");
    }
    return ["/bin/sh", "-lc", command];
  }
  const argv = [...command];
  if (argv.length === 0 || argv.some((part) => part.length === 0)) {
    throw new SandboxValidationError("command argv must contain non-empty strings");
  }
  return argv;
}

export function positiveNumber(name: string, value: number | undefined): number | undefined {
  if (value !== undefined && value < 0) {
    throw new SandboxValidationError(`${name} must be non-negative`);
  }
  return value;
}
