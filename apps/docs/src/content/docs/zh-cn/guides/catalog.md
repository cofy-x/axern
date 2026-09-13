---
title: Catalog
description: 发现平台策展的运行时模板。
---

Catalog 是控制面的运行时模板注册表。模板定义可重建的 Sandbox rootfs 与执行策略。

```bash
axern catalog list
axern catalog get python311
```

`python311`、`coding-base` 等模板为工作负载提供可复现的、平台策展的 rootfs。每个 Run 和 SDK Sandbox 严格选择一个 source：Catalog 模板、通用 OCI 镜像或已有环境。简单试验优先用通用 OCI 镜像；在模板的 Catalog 与复用语义真正重要的地方再引入模板。

把模板或镜像固化成不可变、可复用的 Environment——以及相关的命名空间和配额规则——见 [环境、命名空间与配额](/zh-cn/guides/environments/)。

Agent 与工具镜像由调用方提供。Axrun 接受显式的不可变镜像引用，并通过普通只读 `ImageMount` 能力传给 Axern。
