/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

export interface TunnelSession {
  session_id?: string;
  allocation_id?: string;
  edge_target?: string;
  client_edge_target?: string;
  bound_addr?: string;
  remote_port?: number;
}

export interface CreateTunnelSessionResult {
  session: TunnelSession;
  clientToken: string;
}

export interface TunnelRuntime {
  sessionId: string;
  clientToken: string;
  boundAddr: string;
  upstream: string;
  proxyPort: number;
  stop(): Promise<void>;
}
