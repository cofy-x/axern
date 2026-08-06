---
title: 运行时与资源
description: 在 runsc 与 runc 之间选择，并用 request、limit 和命名空间配额规划工作负载。
---

Axern 的所有工作负载运行在同一套资源和生命周期模型下，运行时类型按工作负载选择。按信任程度而不是运行时长选择类型：

- **`runsc`** 是不可信、Agent 生成代码的推荐隔离边界，在工作负载与主机之间加入用户态内核。
- **`runc`** 面向性能，适合需要完整主机内核兼容性的可信长驻服务。

```bash
axern run --runtime-class runsc docker.io/library/python:3.12-slim -- \
  python -c 'print("hello")'
```

## Request 和 Limit

资源意图分三层：`request` 是调度器和准入的预留量，`limit` 是通过节点本地 cgroup 强制执行的运行时硬上限，命名空间 `quota` 是准入天花板。省略 request 时，控制面默认预留 `500m` CPU 和 `4GiB` 内存。设置了对应 limit 时，不变式为 `0 < request <= limit`。

```bash
axern run --template python311 \
  --request-cpu 500m \
  --request-memory 512MiB \
  --limit-memory 1GiB \
  -- python -c 'print("hello")'
```

Run 和 Service 使用同一套资源 flag；可变的服务资源用 `axern service update` 修改。

## 命名空间配额

配额限制一个命名空间在所有活跃工作负载上可预留的 CPU 和内存总量。省略的字段表示不限制；把配额调低到当前用量之下不会杀死运行中的工作负载，但会阻止新的准入。

```bash
axern namespace create team-a
axern quota set --namespace team-a --cpu 4 --memory 32GiB
axern quota get --namespace team-a
```

配额和节点准入是两道独立的闸门：配额回答命名空间是否还能预留更多，节点准入回答符合条件的节点是否还有剩余容量。两者对内存都是严格的；只有 CPU 可以超卖，且超卖只改变准入容量，从不改变 cgroup limit。

准入失败时，JSON 输出暴露稳定的 `diagnostic_code` 和紧凑的 `admission_summary`，如 `namespace quota exceeded` 或 `node memory capacity exhausted`：

```bash
axern service get <service-id> --output json
```

仓库的 [资源模型](https://github.com/cofy-x/axern/blob/main/docs/architecture/resource-model.md) 是工程层面的权威来源。
