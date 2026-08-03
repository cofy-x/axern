---
title: axern local 参考
description: Local Axern 的环境要求、生命周期、数据、升级与故障诊断。
---

`axern local` 管理一个名为 `local` 的机器级实例。部署资源和服务版本来自
已安装的 CLI，不读取源码仓库文件。Release 二进制内置经验证的多架构镜像
Digest 锁，因此本地启动不会解析可变的服务 Tag。

## 支持范围

首版支持 amd64/arm64 的 macOS 和 Linux，以及 Docker Compose v2；不支持
Windows、Podman、离线安装、多本地实例和自动创建 Kubernetes。

建议至少 4 核 CPU、8 GiB 内存和 20 GiB 可用磁盘。工作负载和 Agent 镜像
首次使用时才拉取；可观测 Profile 需要额外资源。

## 命令

| 命令 | 行为 |
| --- | --- |
| `axern local up` | 预检、生成部署、启动、等待健康并配置 `local` Context |
| `axern local status` | 展示版本、健康、Dashboard、数据路径、Context 和磁盘占用 |
| `axern local logs [component]` | 聚合或指定组件日志，支持 `--follow`、`--tail`、`--since` |
| `axern local doctor` | 只读检查主机、Docker、端口、版本和健康状态 |
| `axern local down` | 删除容器和网络，保留数据 |
| `axern local reset` | 永久删除数据和身份材料 |
| `axern local upgrade` | 备份并显式迁移到 CLI 对应版本 |
| `axern local path` | 输出实际数据目录 |

使用 `axern local up --profile observability` 启用本地可观测组件；使用
`axern local up --profile default` 恢复核心 Profile。不传该参数时保留实例
当前 Profile。

## 数据路径

| 平台 | 默认路径 |
| --- | --- |
| macOS | `~/Library/Application Support/Axern/local` |
| Linux | `${XDG_DATA_HOME:-~/.local/share}/axern/local` |

`AXERN_HOME` 可覆盖根目录。CLI 在其中保存证书、SSH 密钥、Compose 部署、
Secret、数据库/对象数据、元数据和升级备份；敏感文件使用仅所有者可读写权限。

## 本地端口

所有宿主机端口仅绑定 `127.0.0.1`：`25000` 为公开 gRPC Gateway，`25080`
为 Dashboard/HTTP，`25022` 为 SSH，`24101` 为控制面 HTTP，`25432` 为
PostgreSQL，`29000/29001` 为 MinIO。Observability Profile 额外使用
`4317`、`4318` 和 `13000`。

首版不自动分配替代端口；请停止冲突进程后重新运行 `axern local doctor`。

## Context 与代理

`local up` 创建或更新 `local` Context，TLS 和 SSH 均引用 CLI 管理的绝对
路径。只有当前没有 Context 时才自动选中；`--use` 可显式切换。

Runner 会把 `HTTP_PROXY`/`HTTPS_PROXY` 传给容器，并将 loopback 代理地址
改写为 `host.docker.internal`。遇到镜像拉取问题时，先确认 Docker 自身代理
配置，再查看：

```bash
axern local doctor
axern local logs node --tail 200
```

## 升级与卸载

`local up` 不会静默升级。版本不一致时使用 `axern local upgrade`；`status`、
`logs`、`doctor` 和 `down` 仍可使用。升级会停止旧栈并备份数据、身份、元数据
与部署清单；不支持降级。没有受支持迁移路径时需显式 reset。

```bash
axern local reset
brew uninstall axern
```
