/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export function loadLocalComposeEnv(): void {
  if (process.env.AXERN_TS_SMOKE_LOAD_COMPOSE_ENV === "0") {
    return;
  }
  const envPath = findUp("deploy/local/state/compose/axern.env");
  if (envPath === undefined) {
    return;
  }
  for (const line of readFileSync(envPath, "utf8").split(/\r?\n/)) {
    const parsed = parseEnvLine(line);
    if (parsed === undefined || process.env[parsed.key] !== undefined) {
      continue;
    }
    process.env[parsed.key] = parsed.value;
  }
}

export function sandboxSource(): { image: string } | { templateId: string } {
  const image = process.env.AXERN_TS_SMOKE_IMAGE;
  if (image !== undefined && image !== "") {
    return { image };
  }
  return { templateId: process.env.AXERN_TS_SMOKE_TEMPLATE_ID ?? "python311" };
}

function findUp(relativePath: string): string | undefined {
  for (const start of [process.cwd(), __dirname]) {
    let current = start;
    while (true) {
      const candidate = path.join(current, relativePath);
      if (existsSync(candidate)) {
        return candidate;
      }
      const parent = path.dirname(current);
      if (parent === current) {
        break;
      }
      current = parent;
    }
  }
  return undefined;
}

function parseEnvLine(line: string): { key: string; value: string } | undefined {
  const trimmed = line.trim();
  if (trimmed === "" || trimmed.startsWith("#")) {
    return undefined;
  }
  const assignment = trimmed.startsWith("export ") ? trimmed.slice("export ".length).trim() : trimmed;
  const equals = assignment.indexOf("=");
  if (equals <= 0) {
    return undefined;
  }
  const key = assignment.slice(0, equals).trim();
  const rawValue = assignment.slice(equals + 1).trim();
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) {
    return undefined;
  }
  return { key, value: unquote(rawValue) };
}

function unquote(value: string): string {
  if (value.length >= 2 && value.startsWith("\"") && value.endsWith("\"")) {
    return value.slice(1, -1).replace(/\\"/g, "\"").replace(/\\\\/g, "\\");
  }
  if (value.length >= 2 && value.startsWith("'") && value.endsWith("'")) {
    return value.slice(1, -1);
  }
  return value;
}
