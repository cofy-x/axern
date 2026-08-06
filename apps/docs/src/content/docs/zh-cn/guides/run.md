---
title: Run
description: 从镜像、模板或环境执行一次性隔离命令，产出持久记录和真实退出码。
---

Run 是 Axern 的一次性工作负载：在隔离 Sandbox 中执行命令、流式输出、传递命令的真实退出码，并在控制面留下持久记录。长驻 HTTP 工作负载请用 [Service](/zh-cn/guides/service/)；重复的事件处理请用 [Function](/zh-cn/guides/functions/)。

## 运行命令

```bash
axern run python:3.12-slim -- python -c 'print("hello from axern")'
```

CLI 会挂接 stdout/stderr，并以远端命令的真实退出码退出。每次执行都会创建可查询的持久 Run 记录：

```bash
axern run list
axern run get <run-id>
axern run logs <run-id> --follow
axern run cancel <run-id>
```

`run list` 支持按 `--namespace`、`--status`（`queued`、`placed`、`starting`、`running`、`succeeded`、`failed`、`cancelled`）和 `--label` 过滤。`run logs` 支持 `--follow` 和可续读的 `--cursor`；单次读取在 64 MiB 处截断。

## 用 Spec 定义 Run

定义需要评审或复用时，使用严格的 `axern/v1` Spec：

```yaml title="run.yaml"
api_version: axern/v1
kind: Run
metadata:
  namespace: default
  labels:
    example: docs
spec:
  source:
    image: docker.io/library/python:3.12-slim
  command:
    argv: [python, -c, "print('ok')"]
  runtime_class: runsc
  resources:
    requests:
      cpu: 500m
      memory: 512Mi
  env:
    MODE: demo
```

```bash
axern run --file run.yaml
```

`spec.source` 在 `image`、`template`（可配 `template_version`）和 `environment`（已有环境 ID）中严格三选一。私有仓库通过 `registry_credential_id` 引用凭据；凭据只按 ID 引用，绝不写入 Spec。解析器会拒绝未知字段和冲突的 source。

等价的 flag 覆盖同一能力面：`--env`、`--secret-env`、`--secret-file`、`--image-mount`、`--cwd`、`--runtime-class`、`--label`、`--template`、`--environment`，以及四个资源 flag（`--request-cpu`、`--request-memory`、`--limit-cpu`、`--limit-memory`）。`--file` 不能与定义类 flag 混用。

## 后台与长时间运行的 Run

`--detach` 创建 Run 但不跟随输出；`--wait-timeout` 限定 CLI 等待 Run 进入活跃状态的时长（`0` 表示不等待）。Detach 的只是 CLI——Run 会在控制面管理下继续运行到终态，并始终可查询。

Run 状态是持久的。输出流当前由节点本地文件提供，仅在该 Allocation 输出仍被保留时可读；七天期的持久输出保留是一项独立的存储能力。

## 隔离与资源

`runtime_class` 选择隔离边界：不可信代码用 `runsc`，可信的性能优先工作负载用 `runc`。资源 request/limit 与命名空间配额和准入共同生效；模型见[运行时与资源](/zh-cn/architecture/resources/)，查看准入拒绝见 [环境、命名空间与配额](/zh-cn/guides/environments/)。

CLI help 是完整 flag 能力面的权威说明：`axern run --help`。
