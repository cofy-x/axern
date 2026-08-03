---
title: 快速开始
description: 无需克隆源码，在本机安装 Axern 并运行第一个隔离命令。
---

无需克隆仓库，也无需安装 Go、Node.js、Helm、`kubectl`、Make 或
OpenSSL，即可在本机运行完整 Axern。

## 环境要求

- amd64 或 arm64 的 macOS/Linux
- Docker Desktop，或 Docker Engine + Compose v2
- 建议至少 4 核 CPU、8 GiB 内存和 20 GiB 可用磁盘

## 1. 安装 CLI

```bash
brew install cofy-x/tap/axern
```

不使用 Homebrew 时：

```bash
curl -fsSL https://raw.githubusercontent.com/cofy-x/axern/main/install.sh | sh
```

安装器会下载 GitHub Release、严格校验 `checksums.txt`，并默认安装到
用户可写目录。可用 `AXERN_VERSION` 和 `AXERN_INSTALL_DIR` 覆盖默认值。

## 2. 启动 Local Axern

```bash
axern local up
```

该命令会检查 Docker 和主机环境，只启动核心服务，等待 Gateway 与节点
运行时健康，然后创建 `local` Context。运行时与 Agent 镜像会在任务首次
使用时按需拉取。

## 3. 运行命令

```bash
axern run python:3.12-slim -- python -c 'print("hello from axern")'
```

Axern 会把 stdout 和 stderr 实时写入终端，并返回远端命令的真实退出码。
每次执行同时会创建可查询的持久 Run 记录：

```bash
axern run list
axern run logs <run-id>
```

Run 状态会持久保存；当前输出流由节点本地文件提供，仅在该 Allocation 输出
仍被保留时可读。默认保留七天的持久输出属于后续独立存储能力。

## 下一步

- 在 [Local Axern](/zh-cn/getting-started/compose/) 中学习启动、停止、诊断、重置和升级。
- 查看完整的 [`axern local` 参考](/zh-cn/guides/local/)。
- 团队共享与生产部署请使用 [Kubernetes 安装](/zh-cn/getting-started/kubernetes/)。

:::caution[本地开发边界]
本地栈只在 `127.0.0.1` 上公开端口，并为单机开发生成身份材料。不要把它
直接暴露为共享或生产服务。
:::
