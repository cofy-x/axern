---
title: 运行时与资源
description: 用 runsc 隔离工作负载，并用 request、limit 和命名空间配额规划资源。
---

Axern 使用 gVisor（`runsc`）作为生产 sandbox 运行时，在工作负载与主机之间加入用户态内核，使用统一的资源与生命周期模型。执行运行时统一使用 runsc，不提供运行时自动回退。

```bash
axern run docker.io/library/python:3.12-slim -- \
  python -c 'print("hello")'
```

## Request 和 Limit

资源意图分三层：`request` 在准入后成为 Allocation 的不可变资源占用事实，供 placement 与 admission 使用；`limit` 是通过节点本地 cgroup 强制执行的运行时硬上限；命名空间 `quota` 是准入天花板。省略 request 时，控制面应用 `500m` CPU 和 `4GiB` 内存的默认值。设置了对应 limit 时，不变式为 `0 < request <= limit`。Allocation 从绑定开始持续占用资源，经过 `RELEASING` 仍不释放，只有清理完成并进入 `RELEASED` 后容量才会归还。

```bash
axern run python:3.12-slim \
  --request-cpu 500m \
  --request-memory 512MiB \
  --limit-memory 1GiB \
  -- python -c 'print("hello")'
```

每个 Run 在创建时声明自己的资源意图。

## 命名空间配额

配额限制一个命名空间内所有未释放 Allocation 的 CPU 和内存占用总量。省略的字段表示不限制；把配额调低到当前用量之下不会杀死运行中的工作负载，但会阻止新的准入。

```bash
axern namespace create team-a
axern quota set --namespace team-a --cpu 4 --memory 32GiB
axern quota get --namespace team-a
```

配额和节点准入是两道独立的闸门：配额回答命名空间是否还能接受新的 Allocation 占用，节点准入回答符合条件的节点是否还有剩余容量。两者对内存都是严格的；只有 CPU 可以超卖，且超卖只改变准入容量，从不改变 cgroup limit。

准入失败时，JSON 输出暴露稳定的 `diagnostic_code`。随附的 `message` 是面向人的上下文，不应被解析为机器契约：

```bash
axern run get <run-id> --output json
```

仓库的 [资源模型](https://github.com/cofy-x/axern/blob/main/docs/architecture/resource-model.md) 是工程层面的权威来源。
