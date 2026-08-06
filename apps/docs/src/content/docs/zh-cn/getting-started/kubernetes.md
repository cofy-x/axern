---
title: Kubernetes 安装
description: 使用官方 Helm Chart 在 Kubernetes 上安装 Axern 并连接 CLI。
---

Axern 的云中立 Chart 以 OCI Artifact 形式发布在 GHCR。Chart 支持独立的平台、可观测和运行时调度 Profile，生产集群可以把控制面服务与 Sandbox 容量隔离在不同节点池。

本页描述基于本地 port-forward 的评估路径。需要 `kubectl`、Helm 3，以及与 Chart 同一 Release 的 `axern` CLI 归档。继续之前请先从 [Axern Releases](https://github.com/cofy-x/axern/releases) 页面下载归档及其校验和。下文 `<version>` 为 Release 版本号，不带 `v` 前缀。

## 安装 Chart

```bash
helm install axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  --create-namespace \
  --wait \
  --timeout 15m
```

默认镜像是同一 Release 的不可变版本 Tag。环境相关的配置放在自己的 values 文件中，用 `-f values.yaml` 传入。

## 不启用 SSH 连接 CLI

默认 Chart 暴露控制和 HTTP Gateway 端口。在一个终端中保持 port-forward：

```bash
kubectl --namespace axern-system port-forward svc/gatewayd \
  25100:25000 25101:25080
```

在第二个终端中，把 Chart 生成的 mTLS 身份导入为本地 CLI Context。空的 SSH endpoint 是有意为之：Chart 默认禁用 SSH，而 Catalog、Run、Service、Function 和 SDK 工作流都不需要它。

```bash
axern context import-kubernetes local \
  --namespace axern-system \
  --endpoint 127.0.0.1:25100 \
  --service-url http://127.0.0.1:25101 \
  --ssh-endpoint "" \
  --current

axern doctor --namespace default
axern catalog list
```

导入的 Context 携带 endpoint、HTTP service URL 和 TLS 材料。除非显式启用 SSH 并提供客户端身份，SSH 字段保持为空。之后所有控制面工作流与本地 Compose 安装使用同一套 Context 模型。

## 为交互式 Agent 工作流启用 SSH

SSH 是 Gateway 的可选功能，供交互式 Agent 和 SSH 指南使用。用单独的 values 文件配置受信公钥启用：

```yaml title="ssh-values.yaml"
gatewayd:
  ssh:
    enabled: true
    authorizedKeys: |
      ssh-ed25519 AAAA... workstation
```

把 values 文件应用到同一 Release，然后转发 SSH 端口：

```bash
helm upgrade axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  -f ssh-values.yaml \
  --reuse-values \
  --wait \
  --timeout 15m

kubectl --namespace axern-system port-forward svc/gatewayd 25122:25022
```

导入或更新 Context，写入 SSH endpoint 和与受信公钥匹配的私钥：

```bash
axern context import-kubernetes local \
  --namespace axern-system \
  --endpoint 127.0.0.1:25100 \
  --service-url http://127.0.0.1:25101 \
  --ssh-endpoint 127.0.0.1:25122 \
  --ssh-identity-file ~/.ssh/id_ed25519 \
  --current
```

不要在 `authorizedKeys` 为默认值（空）时启用 SSH，否则 Gateway 没有任何可认证的客户端密钥。请限制 SSH 私钥文件权限，并在共享集群使用前审视 host-key 处理方式。

## 持久化部署之前

内置 PostgreSQL 和单节点默认值仅用于评估。运行共享或生产工作负载前，请审视以下 Chart 配置项：

- **Release 产物：** 统一锁定 Chart、镜像和 CLI 版本，安装 CLI 前校验其 checksum。
- **集群前提：** 确认所需的 Kubernetes/Helm 版本、`runsc`/`runc` 运行时可用性、运行时与卷服务所需的节点权限，以及每个调度节点到镜像仓库的可达性。
- **Gateway 暴露：** 用显式管理的 Service 或 Ingress 替换本地 port-forward，配置 TLS 服务器名称和网络策略；除非交互式工作流需要，保持 SSH 关闭。
- **Secret：** 用 `secrets.existingSecret` 提供 master key、rollout worker token、artifact ticket key 和 gateway token；用 `postgres.existingSecret` 提供数据库凭据。
- **持久存储：** 设置 `postgres.persistence.enabled=true` 并搭配拓扑感知的 `ReadWriteOnce` StorageClass；不要在 `emptyDir` 回退上运行持久环境。
- **调度：** 为 `scheduling.platform`、`scheduling.observability` 和 `scheduling.runtime` 配置专用节点池标签和对应的 `NoSchedule` Taint。
- **Rollout worker：** 把 `rolloutWorker.registryAuth.existingSecret` 指向能拉取 TaskSet 仓库的 Docker config secret；Chart 会为 worker 配置独立的控制与执行 mTLS Context。
- **可观测：** 内置的 Prometheus、Tempo、Loki、Grafana 栈是持久的但单副本；在 `observability` 下规划保留周期和存储容量。

:::caution[1.0 前的安全边界]
Axern 不声称默认安装可安全承载不可信的多租户工作负载。TLS、Ingress、镜像信任、网络策略、Secret 存储、配额和持久存储均由运维者负责。
:::

[Helm Chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern) 是 values、节点网络和有状态依赖的权威参考。
