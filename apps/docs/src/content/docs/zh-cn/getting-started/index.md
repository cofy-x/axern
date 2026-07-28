---
title: 入门
description: 选择适合你的 Axern 安装方式和客户端入口。
---

Axern 通过统一的公开 Gateway 运行隔离工作负载。先用 Docker Compose 在一台机器上评估完整控制面和节点运行时；迁移到 Kubernetes 后，继续使用相同的 CLI Context 和 SDK 模型。

## 环境要求

- Docker 与 Compose v2
- GNU Make、curl、OpenSSL 和 SSH 工具
- Linux 主机，或者 macOS 上的 Docker Desktop

Release Quickstart 会下载版本化的多架构镜像和带校验和的 CLI，不要求本机安装 Go、Rust、Python 或 Node.js 工具链。

## 选择路径

| 目标 | 从这里开始 |
| --- | --- |
| 在本地评估 Axern | [Compose 快速开始](/zh-cn/getting-started/compose/) |
| 学习产品命令 | [Axern CLI](/zh-cn/guides/cli/) |
| 构建应用 | [SDK 概览](/zh-cn/sdk/) |
| 运行智能体评估 | [Axrun 托管 Rollout](/zh-cn/axrun/) |
| 安装到 Kubernetes | [Helm Chart 指南](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern) |

:::caution[Pre-1.0 安全边界]
本地生成的凭据和 loopback 监听仅用于开发。共享部署必须自行管理 TLS、Ingress、镜像信任、网络策略、密钥、配额和持久化存储。
:::
