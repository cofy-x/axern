/**
 * @license
 * Copyright 2026 cofy-x
 * SPDX-License-Identifier: Apache-2.0
 */

import assert from "node:assert/strict";
import test from "node:test";
import * as grpc from "@grpc/grpc-js";

import { AxernClient } from "../src/client/index.js";
import type { Workspace } from "../src/client/index.js";
import { AxernRpcError, SandboxStateError } from "../src/errors/index.js";
import { serviceConstructor } from "../src/generated/proto.js";
import { Sandbox } from "../src/sandbox/index.js";

const retained = {
  id: "ws-retained", node_id: "node-a", namespace: "team", status: 2,
  capacity_bytes: "9007199254741504", inode_limit: "9007199254740995",
};

async function transportClient(t: test.TestContext, register: (server: grpc.Server) => void): Promise<AxernClient> {
  const server = new grpc.Server();
  register(server);
  t.after(() => server.forceShutdown());
  const port = await new Promise<number>((resolve, reject) => {
    server.bindAsync("127.0.0.1:0", grpc.ServerCredentials.createInsecure(), (error, port) => {
      if (error) reject(error);
      else resolve(port);
    });
  });
  const client = new AxernClient({ endpoint: `127.0.0.1:${port}`, credentials: grpc.credentials.createInsecure(), proxyMode: "direct" });
  t.after(() => client.close());
  return client;
}

test("Workspace CRUD keeps int64 values and mutations are not retried", async (t) => {
  const WorkspaceControl = serviceConstructor(["axern", "control", "workspace", "v1", "WorkspaceControl"]);
  const RunControl = serviceConstructor(["axern", "control", "run", "v1", "RunControl"]);
  let createCalls = 0;
  let deleteCalls = 0;
  let unavailable = false;
  let createRequest: Record<string, unknown> = {};
  let listRequest: Record<string, unknown> = {};
  let runRequest: Record<string, unknown> = {};
  const client = await transportClient(t, (server) => {
    server.addService(WorkspaceControl.service, {
      CreateWorkspace(call: grpc.ServerUnaryCall<Record<string, unknown>, unknown>, callback: grpc.sendUnaryData<unknown>) {
        createCalls++;
        createRequest = call.request;
        if (unavailable) callback({ code: grpc.status.UNAVAILABLE, details: "inert transport failure" });
        else callback(null, { workspace: retained });
      },
      GetWorkspace(_call: unknown, callback: grpc.sendUnaryData<unknown>) { callback(null, { workspace: retained }); },
      ListWorkspaces(call: grpc.ServerUnaryCall<Record<string, unknown>, unknown>, callback: grpc.sendUnaryData<unknown>) {
        listRequest = call.request;
        callback(null, { workspaces: [retained], next_cursor: "next" });
      },
      DeleteWorkspace(_call: unknown, callback: grpc.sendUnaryData<unknown>) {
        deleteCalls++;
        if (unavailable) callback({ code: grpc.status.UNAVAILABLE, details: "inert transport failure" });
        else callback(null, { workspace: { ...retained, status: 4 } });
      },
    });
    server.addService(RunControl.service, {
      AdmitRun(call: grpc.ServerUnaryCall<Record<string, unknown>, unknown>, callback: grpc.sendUnaryData<unknown>) {
        runRequest = call.request;
        callback(null, { run: { id: "run-a" } });
      },
    });
  });
  const value = await client.createWorkspace({ nodeId: "node-a", capacityBytes: 9007199254741504n, inodeLimit: "9007199254740995" });
  assert.equal(value.capacityBytes, "9007199254741504");
  assert.equal(value.inodeLimit, "9007199254740995");
  assert.equal(createRequest.capacity_bytes, "9007199254741504");
  assert.equal(createRequest.inode_limit, "9007199254740995");
  assert.equal((await client.getWorkspace("ws-retained")).id, "ws-retained");
  assert.equal((await client.listWorkspaces({ namespace: "team", cursor: "current", pageSize: 7 })).nextCursor, "next");
  assert.equal(listRequest.namespace, "team");
  assert.equal(listRequest.cursor, "current");
  assert.equal((await client.deleteWorkspace("ws-retained")).status, "deleting");
  await client.createRun({ environmentId: "env-a", workspaceId: "ws-retained" });
  assert.equal(runRequest.workspace_id, "ws-retained");
  await assert.rejects(() => client.createWorkspace({ nodeId: "node-a", capacityBytes: 9007199254740993, inodeLimit: 64 }));
  await assert.rejects(() => client.createWorkspace({ nodeId: "node-a", capacityBytes: 513, inodeLimit: 64 }));
  assert.equal(createCalls, 1, "invalid capacity reached transport");
  unavailable = true;
  await assert.rejects(() => client.createWorkspace({ nodeId: "node-a", capacityBytes: 4096, inodeLimit: 64 }));
  await assert.rejects(() => client.deleteWorkspace("ws-retained"));
  assert.equal(createCalls, 2, "create mutation was retried");
  assert.equal(deleteCalls, 2, "delete mutation was retried");
});

test("required Workspace admission cannot invoke an old CreateRun-only service", async (t) => {
  const RunControl = serviceConstructor(["axern", "control", "run", "v1", "RunControl"]);
  let legacyCalls = 0;
  const client = await transportClient(t, (server) => {
    const admit = RunControl.service.AdmitRun;
    server.addService({ CreateRun: { ...admit, path: "/axern.control.run.v1.RunControl/CreateRun" } }, {
      CreateRun(_call: unknown, callback: grpc.sendUnaryData<unknown>) {
        legacyCalls++;
        callback(null, { run: { id: "unexpected-ephemeral" } });
      },
    });
  });
  await assert.rejects(() => client.createRun({ environmentId: "env-a", workspaceId: "ws-retained" }),
    (error: unknown) => error instanceof AxernRpcError && error.code === String(grpc.status.UNIMPLEMENTED));
  assert.equal(legacyCalls, 0);
});

test("Sandbox close cancels execution and preserves independent Workspace data", async () => {
  let selected = "";
  let cancelled = false;
  let deleted = false;
  const client = {
    async createRun(options: { workspaceId?: string }) { selected = options.workspaceId ?? ""; return { id: "run-a" }; },
    async *watchRun() { yield { id: "run-a", allocation_id: "alloc-a", status: 3,
      workspace_attachment: { workspace_id: "ws-retained", generation: "9007199254740993" } }; },
    async cancelRun() { cancelled = true; },
    async deleteWorkspace() { deleted = true; },
    close() {},
  } as unknown as AxernClient;
  const sandbox = await new Sandbox({ client, environmentId: "env-a", workspaceId: "ws-retained" }).start();
  assert.equal(selected, "ws-retained");
  assert.equal(sandbox.metadata.workspaceId, "ws-retained");
  assert.equal(sandbox.metadata.workspaceGeneration, "9007199254740993");
  await sandbox.close();
  assert.equal(cancelled, true);
  assert.equal(deleted, false);
});

test("ready waiter observes cleanup without acquiring another writer", async () => {
  const client = Object.create(AxernClient.prototype) as AxernClient;
  const value: Workspace = { id: "ws-a", namespace: "team", nodeId: "node-a", status: "ready", capacityBytes: "4096", inodeLimit: "64", generation: "1", attachedAllocationId: "alloc-a", usedBytes: "0", usedInodes: "0", message: "", labels: {} };
  let observations = 0;
  client.getWorkspace = async () => ({ ...value, attachedAllocationId: ++observations === 1 ? "alloc-a" : "" });
  assert.equal((await client.waitWorkspaceReady("ws-a", 1000)).attachedAllocationId, "");
  assert.equal(observations, 2);
  client.getWorkspace = async () => ({ ...value, id: "different", attachedAllocationId: "" });
  await assert.rejects(() => client.waitWorkspaceReady("ws-a", 1000), SandboxStateError);
  client.getWorkspace = async () => ({ ...value, status: "unavailable" });
  await assert.rejects(() => client.waitWorkspaceReady("ws-a", 1000), SandboxStateError);
});
