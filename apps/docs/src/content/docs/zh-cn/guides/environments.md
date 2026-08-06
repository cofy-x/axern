---
title: 环境、命名空间与配额
description: 复用不可变环境，按命名空间组织工作负载，并查看配额与准入信号。
---

Environment 是不可变、可复用的执行源：解析好的 Catalog 模板或 OCI 镜像引用，Run、Service 和 Sandbox 可以共享它而不必重复解析镜像。命名空间组织资源，配额限制各命名空间可准入的用量。

## 创建和复用 Environment

```bash
axern environment create --template-id python311
axern environment create --image-ref docker.io/library/python:3.12-slim
axern environment list
axern environment get <environment-id>
axern environment delete <environment-id>
```

环境严格二选一 source：来自 [Catalog](/zh-cn/guides/catalog/) 的 `--template-id`（可配 `--template-version`），或 `--image-ref`。私有仓库使用按 ID 引用的已存凭据：

```bash
axern environment create \
  --image-ref registry.example.com/team/base:1.0 \
  --registry-credential-id <secret-id>
```

环境是不可变的——镜像或模板变更意味着创建新环境。

把环境 ID 传给任意工作负载，避免重复解析 source：

```bash
axern run --environment <environment-id> -- python -c 'print("ok")'
axern service create --environment-id <environment-id> \
  --argv=python --argv=-m --argv=http.server --argv=8080
```

SDK 构造 Sandbox 时接受同样的 `environment_id` source，Agent Workspace 正是借此跨会话保持稳定环境。

## 命名空间

命名空间隔离资源并限定角色绑定范围：

```bash
axern namespace list
axern namespace create team-a
axern namespace get team-a
axern namespace delete team-a
```

工作负载命令默认使用 `default` 命名空间，可用 `--namespace` 指定。只有不活跃的命名空间可以删除。命名空间内的访问遵循 [身份与命名空间访问](/zh-cn/guides/authorization/)中的角色模型。

## 配额与准入

配额限制命名空间可准入的 CPU 和内存。超出剩余配额的工作负载会在准入时被拒绝，并携带诊断码：

```bash
axern quota get --namespace default
axern quota events --namespace default
```

`quota get` 报告配置的配额、当前用量和准入信号；`quota events` 列出最近的准入决策。平台工具用 `quota list --pressure` 找出受限的命名空间，用 `quota set`/`quota unset` 调整配置的限额。

底层的 request/limit 模型、准入阶段和诊断码见 [运行时与资源](/zh-cn/architecture/resources/)，以及仓库的 [资源模型](https://github.com/cofy-x/axern/blob/main/docs/architecture/resource-model.md)。
