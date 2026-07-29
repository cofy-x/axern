import { existsSync, writeFileSync } from "node:fs";
import path from "node:path";
import { AXERN_VERSION, AxernClient, Sandbox } from "@cofy-x/axern-sdk";

const config = required("AXERN_SDK_ACCEPTANCE_CONFIG");
const context = required("AXERN_SDK_ACCEPTANCE_CONTEXT");
const version = required("AXERN_SDK_ACCEPTANCE_VERSION");
const handshake = required("AXERN_SDK_ACCEPTANCE_HANDSHAKE_DIR");
const marker = "axern-typescript-sdk-release-ok";
if (AXERN_VERSION !== version) throw new Error(`unexpected TypeScript SDK version: ${AXERN_VERSION}`);

const client = AxernClient.fromContext(config, context);
let sandbox;
try {
  sandbox = await new Sandbox({
    client,
    templateId: "python311",
    runtimeClass: "runsc",
    labels: { "axern.release.acceptance": "typescript" },
  }).start();
  const result = await sandbox.exec(["python", "-c", `print(${JSON.stringify(marker)})`], { check: true });
  if (result.stdoutText().trim() !== marker) {
    throw new Error(`unexpected TypeScript SDK exec output: ${JSON.stringify(result.stdoutText())}`);
  }
  writeFileSync(path.join(handshake, "typescript.service-id"), sandbox.metadata.serviceId, "utf8");
  await waitVerified(path.join(handshake, "typescript.verified"));
  console.log(`sdk_data_plane=typescript service_id=${sandbox.metadata.serviceId} ok=true`);
} finally {
  await sandbox?.close();
  client.close();
}

async function waitVerified(verifiedPath) {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (existsSync(verifiedPath)) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("CLI did not verify the TypeScript SDK service");
}

function required(name) {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}
