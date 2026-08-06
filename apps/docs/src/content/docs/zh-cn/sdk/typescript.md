---
title: TypeScript SDK
description: 用 Node.js 创建和编程 Axern Sandbox。
---

TypeScript SDK 以 Node.js 为先，使用 Promise API。

```bash
pnpm add @cofy-x/axern-sdk@<version>
```

官方包发布在 [npm 的 `@cofy-x/axern-sdk`](https://www.npmjs.com/package/@cofy-x/axern-sdk)。

```typescript
import { AxernClient, Sandbox } from "@cofy-x/axern-sdk";

const client = AxernClient.fromContext(
  process.env.AXERN_CONFIG ?? `${process.env.HOME}/.config/axern/config.json`,
  process.env.AXERN_CONTEXT,
);

const sandbox = await new Sandbox({
  client,
  image: "docker.io/library/python:3.12-slim",
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

`AxernClient.fromContext()` 适合交互式工具；`AxernClient.fromEnv()` 是面向自动化的显式环境变量路径。构造函数不会隐式读取用户主目录。能力查询和 Computer Use 直接挂在 `Sandbox` 上：`capabilityStatus()`、`computerUseStatus()`，以及 display、screenshot、mouse、keyboard 方法。

- [TypeScript SDK 源码与完整指南](https://github.com/cofy-x/axern/tree/main/sdk/typescript)
- [可编程示例](https://github.com/cofy-x/axern/blob/main/sdk/typescript/examples/programmable.ts)
