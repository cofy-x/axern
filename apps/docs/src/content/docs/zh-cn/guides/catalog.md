---
title: Catalog
description: 发现平台策展的运行时模板和 Agent Bundle。
---

Catalog 是控制面的运行时模板和 Agent Bundle 注册表。模板定义 Sandbox rootfs；Bundle 定义挂载进 Sandbox 的只读 Agent 或工具镜像。

```bash
axern catalog list
axern catalog get python311
axern catalog bundle list
axern catalog bundle get <bundle-id>
```

`python311`、`coding-base` 等模板为工作负载提供可复现的、平台策展的 rootfs。每个 Run、Service、Function 和 SDK Sandbox 严格选择一个 source：Catalog 模板、通用 OCI 镜像或已有环境。简单试验优先用通用 OCI 镜像；在模板的 Catalog 与复用语义真正重要的地方再引入模板。

把模板或镜像固化成不可变、可复用的 Environment——以及相关的命名空间和配额规则——见 [环境、命名空间与配额](/zh-cn/guides/environments/)。

Agent Bundle 通常是隐式解析的：`axern agent` 挂载匹配的 `codex` 或 `claude-code` Bundle，Axrun 则在 Rollout 规划时冻结 Bundle Digest。
