/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

export class AxernError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "AxernError";
  }
}

export class AxernRpcError extends AxernError {
  readonly code?: number | string;
  readonly operation: string;
  readonly allocationId?: string;
  readonly details: string;
  readonly retryable: boolean;

  constructor(
    operation: string,
    cause: unknown,
    options: { allocationId?: string } = {},
  ) {
    const detail = errorDetail(cause);
    super(`${operation} failed${detail ? `: ${detail}` : ""}`, { cause });
    this.name = "AxernRpcError";
    this.operation = operation;
    this.allocationId = options.allocationId;
    this.details = detail;
    this.code = typeof cause === "object" && cause !== null && "code" in cause
      ? String((cause as { code?: unknown }).code)
      : undefined;
    this.retryable = isRetryableRpcCode(rpcCode(this));
  }
}

export class SandboxTimeoutError extends AxernError {
  constructor(message: string) {
    super(message);
    this.name = "SandboxTimeoutError";
  }
}

export class SandboxStateError extends AxernError {
  constructor(message: string) {
    super(message);
    this.name = "SandboxStateError";
  }
}

export class SandboxValidationError extends AxernError {
  constructor(message: string) {
    super(message);
    this.name = "SandboxValidationError";
  }
}

export class SandboxExecError extends AxernError {
  readonly argv: string[];
  readonly exitCode: number;
  readonly stdout: Buffer;
  readonly stderr: Buffer;

  constructor(argv: string[], result: { exitCode: number; stdout: Buffer; stderr: Buffer }) {
    super(`sandbox command exited with code ${result.exitCode}: ${argv.join(" ")}`);
    this.name = "SandboxExecError";
    this.argv = [...argv];
    this.exitCode = result.exitCode;
    this.stdout = result.stdout;
    this.stderr = result.stderr;
  }
}

export function mapRpcError(error: unknown, operation: string, allocationId?: string): AxernRpcError {
  return new AxernRpcError(operation, error, { allocationId });
}

export function isNotFound(error: unknown): boolean {
  return rpcCode(error) === 5;
}

export function isPermissionDenied(error: unknown): boolean {
  const code = rpcCode(error);
  return code === 7 || code === 16;
}

export function isTimeout(error: unknown): boolean {
  return error instanceof SandboxTimeoutError || rpcCode(error) === 4;
}

export function isCancelled(error: unknown): boolean {
  return rpcCode(error) === 1;
}

export function isUnavailable(error: unknown): boolean {
  return rpcCode(error) === 14;
}

export function errorRetryable(error: unknown): boolean {
  if (error instanceof AxernRpcError) {
    return error.retryable;
  }
  return isRetryableRpcCode(rpcCode(error));
}

export function rpcCode(error: unknown): number | undefined {
  const code = rawRpcCode(error);
  if (code === undefined) {
    return undefined;
  }
  const numeric = Number(code);
  return Number.isNaN(numeric) ? undefined : numeric;
}

function rawRpcCode(error: unknown): unknown {
  if (typeof error !== "object" || error === null) {
    return undefined;
  }
  const candidate = error as { code?: unknown; cause?: unknown };
  if (candidate.code !== undefined) {
    return candidate.code;
  }
  return rawRpcCode(candidate.cause);
}

function isRetryableRpcCode(code: number | undefined): boolean {
  return code === 4 || code === 14;
}

function errorDetail(error: unknown): string {
  if (typeof error === "object" && error !== null) {
    const maybeDetails = error as { details?: unknown; message?: unknown };
    if (typeof maybeDetails.details === "string" && maybeDetails.details.length > 0) {
      return maybeDetails.details;
    }
    if (typeof maybeDetails.message === "string" && maybeDetails.message.length > 0) {
      return maybeDetails.message;
    }
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "";
}
