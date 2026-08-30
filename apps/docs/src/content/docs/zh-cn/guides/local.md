---
title: Local Axern 参考
description: Local Axern 的环境要求、生命周期、数据、升级与故障诊断。
---

`axern local` 管理一个名为 `local` 的机器级实例。部署资源和服务版本来自已安装的 CLI，不读取源码仓库文件。Release 二进制内置经验证的多架构镜像 Digest 锁，因此本地启动不会解析可变的服务 Tag。

本页是完整参考。首次上手请从 [Local Axern](/zh-cn/getting-started/compose/) 教程开始。

## 支持范围

首版支持 amd64/arm64 的 macOS 和 Linux，以及 Docker Compose v2；不支持 Windows、Podman、离线安装、多本地实例和自动创建 Kubernetes。

建议至少 4 核 CPU、8 GiB 内存和 20 GiB 可用磁盘。工作负载和 Agent 镜像首次使用时才拉取；可观测 Profile 需要额外资源。

## 命令

| 命令 | 行为 |
| --- | --- |
| `axern local up` | 预检、生成部署、启动、等待健康并配置 `local` Context |
| `axern local image load IMAGE` | 将宿主 Docker 镜像流式导入本地节点；`--pull` 会先拉取镜像 |
| `axern local status` | 展示版本、健康、Dashboard、数据路径、Context 和磁盘占用 |
| `axern local logs [component]` | 聚合或指定组件日志，支持 `--follow`、`--tail`、`--since` |
| `axern local doctor` | 只读检查主机、Docker、端口、版本、健康状态和 Node DNS；`--probe` 额外验证 Sandbox DNS |
| `axern local down` | 删除容器和网络，保留数据 |
| `axern local reset` | 永久删除数据和身份材料 |
| `axern local upgrade` | 备份并显式迁移到 CLI 对应版本 |
| `axern local path` | 输出实际数据目录 |

使用 `axern local up --profile observability` 启用本地可观测组件；使用 `axern local up --profile default` 恢复核心 Profile。不传该参数时保留实例当前 Profile。

镜像导入仅适用于本地模式。CLI 按不可变宿主镜像 ID 保存镜像，核对其平台与运行中的 Node 镜像，并直接流式传输，不在宿主生成归档文件。同一可变 tag 重建后，新 allocation 会使用新的 manifest generation；运行中的 allocation 继续持有已租用的旧 generation。`local` CLI context 只记录当前可变 tag 指针，并向控制面提交其不可变 digest，因此从已导入镜像创建 Run 或 Service 时不会访问外部 registry。

## 数据路径

| 平台 | 默认路径 |
| --- | --- |
| macOS | `~/Library/Application Support/Axern/local` |
| Linux | `${XDG_DATA_HOME:-~/.local/share}/axern/local` |

`AXERN_HOME` 可覆盖根目录。CLI 在其中保存证书、SSH 密钥、Compose 部署、Secret、数据库/对象数据、元数据和升级备份；敏感文件使用仅所有者可读写权限。

## 本地端口

所有宿主机端口仅绑定 `127.0.0.1`：`25000` 为公开 gRPC Gateway，`25080` 为 Dashboard/HTTP，`25022` 为 SSH，`24101` 为控制面 HTTP，`25432` 为 PostgreSQL，`29000/29001` 为 MinIO。Observability Profile 额外使用 `4317`、`4318` 和 `13000`。

首版不自动分配替代端口；请停止冲突进程后重新运行 `axern local doctor`。

## Context 与代理

`local up` 创建或更新 `local` Context，TLS 和 SSH 均引用 CLI 管理的绝对路径。只有当前没有 Context 时才自动选中；`--use` 可显式切换。

Runner 会把 `HTTP_PROXY`/`HTTPS_PROXY` 传给容器，并将 loopback 代理地址改写为 `host.docker.internal`。遇到镜像拉取问题时，先确认 Docker 自身代理配置，再查看：

```bash
axern local doctor
axern local logs node --tail 200
```

## 工作负载 DNS

默认情况下，axnoded 会从 Node 容器的实际 Resolver 配置派生非 loopback 上游，供 OCI 工作负载使用。Docker 容器内的 loopback Resolver 无法从嵌套 Sandbox 访问，因此不会直接复制；如果 Docker 提供外部上游元数据，axnoded 会使用其中可达的地址。

`axern local doctor` 会验证已初始化 Stack 实际使用的 materialized `compose.env`（`runtime_dns_config`），并从运行中的 Node 容器直接查询 axnoded 的每一个有效 Resolver（`runtime_dns_node`）。materialized override 为空表示由 axnoded 从 Node 环境派生，并不是无效配置。这两项检查都是只读操作，默认超时为 15 秒，可用 `--check-timeout` 调整。

要通过真实 `runsc` OCI Sandbox 和正常公共 API 路径验证 DNS，请显式运行：

```bash
axern local doctor --probe
```

Sandbox 检查（`runtime_dns_sandbox`）会通过公共 API 创建临时 Namespace、Secret、Environment 和 Run。成功、失败、超时或取消后，清理逻辑会先取消仍在运行的 Run，再按依赖顺序删除 Environment、Secret 和 Namespace；已终止的 Run 仍作为正常控制面历史保留。默认查询项目控制的绝对域名 `axern.cofy-x.space.`；企业网络可通过 `--dns-query-name` 改为私有域名。查询目标通过临时 Secret 注入，不会出现在 Run 参数、doctor JSON details 或 probe 输出中。

Probe 始终连接产品管理的 `local` Context，忽略当前选中的远程 Context，并拒绝显式远程 Endpoint 或 TLS 覆盖。Sandbox 默认超时为 5 分钟，可用 `--probe-timeout` 调整；默认 Template 为 `python311`，Runtime Class 为 `runsc`，这些 Sandbox 专用参数只能与 `--probe` 一起使用。清理失败属于 required failure，应先检查带 doctor probe 标签的本地资源再重试。

VPN 或企业网络有时要求使用 Node 容器实际配置中不可见的 DNS。可在启动或重建本地栈前显式设置逗号分隔的 Resolver IP：

```bash
AXERN_LOCAL_DNS_NAMESERVERS=10.0.0.53,10.0.0.54 axern local up
```

这些值必须是 Docker 工作负载可访问的 IP 地址；loopback、未指定地址、空值和主机名都会被拒绝。运行中的实例修改 DNS 后，执行 `axern local down` 再执行 `axern local up`，即可在保留数据的同时重建 Node 容器。

## 升级与卸载

`local up` 不会静默升级。版本不一致时使用 `axern local upgrade`；`status`、`logs`、`doctor` 和 `down` 仍可使用。升级会停止旧栈并备份数据、身份、元数据与部署清单；不支持降级。没有受支持迁移路径时需显式 reset。

```bash
axern local reset
brew uninstall axern
```
