/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import * as grpc from "@grpc/grpc-js";

import { serviceConstructor } from "../generated/proto.js";

export interface NodeClientContextOptions {
  allocationId: string;
  target: string;
  credentials: grpc.ChannelCredentials;
  channelOptions?: grpc.ChannelOptions;
}

export class NodeClientContext {
  readonly allocationId: string;

  private readonly target: string;
  private readonly credentials: grpc.ChannelCredentials;
  private readonly channelOptions: grpc.ChannelOptions;

  constructor(options: NodeClientContextOptions) {
    this.allocationId = options.allocationId;
    this.target = options.target;
    this.credentials = options.credentials;
    this.channelOptions = options.channelOptions ?? {};
  }

  async withAuthRetry<T>(
    _leaseTtlSeconds: number,
    operation: (client: grpc.Client) => Promise<T>,
  ): Promise<T> {
    const client = this.rpcClient();
    try {
      return await operation(client);
    } finally {
      client.close();
    }
  }

  rpcClient(): grpc.Client {
    const NodeSandbox = serviceConstructor(["axern", "node", "sandbox", "v1", "NodeSandbox"]);
    return new NodeSandbox(this.target, this.credentials, this.channelOptions);
  }

  authRequest(payload: Record<string, unknown>): Record<string, unknown> {
    return {
      allocation_id: this.allocationId,
      ...payload,
    };
  }
}
