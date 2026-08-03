/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import path from "node:path";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";

import type { Dict } from "../types.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const defaultProtoRootCandidates = [
  path.resolve(__dirname, "../proto"),
  path.resolve(__dirname, "../../../proto"),
];

const protoFiles = [
  "axern/control/environment/v1/environment.proto",
  "axern/control/run/v1/run.proto",
  "axern/control/service/v1/service.proto",
  "axern/control/tunnel/v1/tunnel.proto",
  "axern/node/sandbox/v1/node.proto",
  "axern/tunnel/v1/tunnel.proto",
];

let loadedPackage: grpc.GrpcObject | undefined;

export function axernProtoRoot(): string {
  if (process.env.AXERN_PROTO_ROOT !== undefined && process.env.AXERN_PROTO_ROOT !== "") {
    return process.env.AXERN_PROTO_ROOT;
  }
  const protoRoot = defaultProtoRootCandidates.find((candidate) =>
    existsSync(path.join(candidate, "axern/control/environment/v1/environment.proto"))
  );
  if (protoRoot === undefined) {
    throw new Error("AXERN_PROTO_ROOT is required because bundled proto files were not found");
  }
  return protoRoot;
}

export function loadAxernPackage(): grpc.GrpcObject {
  if (loadedPackage !== undefined) {
    return loadedPackage;
  }
  const protoRoot = axernProtoRoot();
  const definition = protoLoader.loadSync(
    protoFiles.map((file) => path.join(protoRoot, file)),
    {
      includeDirs: [protoRoot],
      keepCase: true,
      longs: String,
      enums: Number,
      defaults: false,
      oneofs: true,
    },
  );
  loadedPackage = grpc.loadPackageDefinition(definition);
  return loadedPackage;
}

export function serviceConstructor(pathParts: string[]): grpc.ServiceClientConstructor {
  let current: unknown = loadAxernPackage();
  for (const part of pathParts) {
    current = (current as Dict)[part];
    if (current === undefined) {
      throw new Error(`missing proto service path: ${pathParts.join(".")}`);
    }
  }
  return current as grpc.ServiceClientConstructor;
}

export function unary<TRequest extends object, TResponse>(
  client: grpc.Client,
  method: string,
  request: TRequest,
  deadlineMs?: number,
): Promise<TResponse> {
  return new Promise((resolve, reject) => {
    const callback = (error: grpc.ServiceError | null, response: TResponse) => {
      if (error) {
        reject(error);
      } else {
        resolve(response);
      }
    };
    const options = deadlineMs === undefined ? undefined : { deadline: Date.now() + deadlineMs };
    const fn = (client as unknown as Dict)[method] as Function;
    if (options === undefined) {
      fn.call(client, request, callback);
    } else {
      fn.call(client, request, options, callback);
    }
  });
}
