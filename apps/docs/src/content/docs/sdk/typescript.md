---
title: TypeScript SDK
description: Create and program an Axern sandbox from Node.js.
---

The TypeScript SDK is Node.js-first and uses Promise APIs.

```typescript
import { AxernClient, Sandbox } from "@cofy-x/axern-sdk";

const client = AxernClient.fromContext(
  process.env.AXERN_CONFIG ?? `${process.env.HOME}/.config/axern/config.json`,
  process.env.AXERN_CONTEXT,
);

const sandbox = await new Sandbox({
  client,
  templateId: "python311",
}).start();

try {
  const result = await sandbox.exec("python -c \"print('hello from Node.js')\"", {
    check: true,
  });
  console.log(result.stdoutText());
} finally {
  await sandbox.close();
  client.close();
}
```

`AxernClient.fromContext()` is appropriate for interactive tools;
`AxernClient.fromEnv()` is the explicit environment-driven path for
automation. Constructors do not silently read the user's home directory.

- [TypeScript SDK source and full guide](https://github.com/cofy-x/axern/tree/main/sdk/typescript)
- [Programmable example](https://github.com/cofy-x/axern/blob/main/sdk/typescript/examples/programmable.ts)
