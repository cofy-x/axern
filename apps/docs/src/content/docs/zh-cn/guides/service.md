---
title: Service
description: 运行长驻 HTTP 工作负载，支持副本、就绪探针、滚动更新和 Gateway 路由。
---

Service 是 Axern 的长驻工作负载：维持目标副本数健康、把配置变更滚动到各副本，并通过公开的 `/svc` Gateway 路由暴露容器端口。一次性命令请用 [Run](/zh-cn/guides/run/)；事件处理请用 [Function](/zh-cn/guides/functions/)。

## 创建 Service

```bash
axern service create --file service.yaml --wait
```

Service Spec 使用与 Run 相同的严格 `axern/v1` 封装，并增加副本和探针语义：

```yaml title="service.yaml"
api_version: axern/v1
kind: Service
metadata:
  namespace: default
spec:
  source:
    image: docker.io/library/python:3.12-slim
  command:
    argv: [python, -m, http.server, "8080"]
  runtime_class: runc
  replicas: 2
  readiness:
    http:
      port: 8080
      path: /
  resources:
    requests:
      cpu: 500m
      memory: 512Mi
```

`spec.source` 在 `image`、`template` 和 `environment` 中严格三选一。`readiness` 和 `liveness` 接受 `http` 探针（`port`、`path`、`scheme`）或 `tcp_port`，以及 `initial_delay`、`period`、`timeout`、`success_threshold`、`failure_threshold` 时长。`autoscaling` 设置 `min_replicas` 和 `max_replicas`。可信的长驻服务用 `runc`，处理不可信输入的工作负载用 `runsc`；见[运行时与资源](/zh-cn/architecture/resources/)。

## 通过 Gateway 访问 Service

Gateway 按端口号或端口名把 HTTP 流量路由到就绪副本：

```text
<context.service_url>/svc/<namespace>/<service-id>/<port>/<path>
```

Service 状态以控制面为准；Gateway 只做解析和转发，不掌管调度。

## 查看和更新

```bash
axern service list --namespace default
axern service get <service-id> --output json
axern service replicas <service-id>
axern service events <service-id>
```

Service 会报告 `reconciling`、`ready`、`degraded`、`failed`、`deleting` 或 `deleted` 之一。`service replicas` 可按 `all`、`current`、`updated`、`outdated`、`unhealthy` 或 `ended` 查看副本 Allocation。

更新是带乐观并发控制的显式滚动：

```bash
axern service update <service-id> --replicas 3
axern service update <service-id> --environment-id <environment-id> \
  --max-surge 1 --max-unavailable 0
axern service delete <service-id>
```

一次更新可以变更副本数、源环境、执行配置或滚动策略，并据此替换副本；滚动排水期间，副本视图会区分 `updated` 与 `outdated` Allocation。多个操作方更新同一 Service 时，用 `--expected-version` 做乐观并发控制。

## 向副本建立隧道

需要让本地上游出现在 Service 网络内时，向一个就绪副本开启反向隧道：

```bash
axern service tunnel <service-id> --to 127.0.0.1:8080
```

会话生命周期、查看和吊销见[反向隧道](/zh-cn/guides/tunnels/)。包含 Gateway 验证和清理的完整 SDK 演练见 [Python Service](/zh-cn/guides/python-service/)。
