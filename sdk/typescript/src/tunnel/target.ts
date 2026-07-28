/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import type net from "node:net";

import { SandboxValidationError } from "../errors/index.js";

export function parseTcpTarget(target: string): net.NetConnectOpts {
  if (target.startsWith("[")) {
    const end = target.indexOf("]");
    if (end === -1 || target[end + 1] !== ":") {
      throw new SandboxValidationError(`invalid tunnel upstream target: ${target}`);
    }
    const port = Number(target.slice(end + 2));
    if (!Number.isInteger(port) || port <= 0) {
      throw new SandboxValidationError(`invalid tunnel upstream port: ${target}`);
    }
    return { host: target.slice(1, end), port };
  }
  const colon = target.lastIndexOf(":");
  if (colon <= 0 || colon === target.length - 1) {
    throw new SandboxValidationError(`invalid tunnel upstream target: ${target}`);
  }
  const port = Number(target.slice(colon + 1));
  if (!Number.isInteger(port) || port <= 0) {
    throw new SandboxValidationError(`invalid tunnel upstream port: ${target}`);
  }
  return { host: target.slice(0, colon), port };
}
