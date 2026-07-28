/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

export function debugFrame(direction: string, frame: Record<string, unknown>): void {
  if (process.env.AXERN_TS_TUNNEL_DEBUG !== "1") {
    return;
  }
  const summary = { ...frame };
  if ("stream_data" in summary) {
    const data = summary.stream_data as Record<string, unknown>;
    summary.stream_data = {
      ...data,
      data: Buffer.from((data.data as Buffer | Uint8Array | undefined) ?? []).length,
    };
  }
  process.stderr.write(`[axern-ts-tunnel] ${direction} ${JSON.stringify(summary)}\n`);
}
