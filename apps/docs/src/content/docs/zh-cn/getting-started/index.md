---
title: 快速开始
description: 从一致的源码构建启动 Axern，并选择原生 serving 或 Compose 开发路径。
---

Axern 的当前源码包含需要一起更新的 CLI、服务和 SDK 协议。源码版本不代表安装包已经发布；请使用同一构建版本，不要将旧发布版客户端与当前服务混用。

## Linux 单机 serving

按仓库的[原生安装指南](https://github.com/cofy-x/axern/blob/main/deploy/standalone/INSTALL.md)构建并安装完整归档。安装目标不需要源码、编译器、Docker 或 Kubernetes。启用持久 Workspace 时，需要独立 XFS 数据挂载、项目配额和明确的容量预算。

安装后，通过归档内的 CLI 管理当前主机：

```bash
sudo /opt/axern/releases/<archive-sha256>/bin/axern server start
sudo /opt/axern/releases/<archive-sha256>/bin/axern server status
sudo /opt/axern/releases/<archive-sha256>/bin/axern server logs axnoded
```

API Context 选择后续执行请求的服务端。它不会把 `server` 命令变成远程主机管理。

## Compose 源码开发

需要 Go 1.26.8、Make，以及 macOS/Linux 上的 Docker Compose v2。建议至少 4 核 CPU、8 GiB 内存和 20 GiB 可用磁盘。在源码仓库根目录运行：

```bash
make quickstart-source
make axern-cli-build
./bin/axern context list
```

该路径构建当前源码服务镜像并执行 Compose smoke。连接和 Context 说明见[本地部署入口](https://github.com/cofy-x/axern/blob/main/deploy/local/README.md)。开发栈和原生 serving 是不同安装，不共享数据。

发布版 CLI 的内置开发 bundle 使用 `axern dev up|status|logs|down`，需要匹配的服务镜像；它不会读取当前源码构建。完整参考见 [Compose 开发](/zh-cn/getting-started/compose/)。

## 使用执行与保留数据

- [Run](/zh-cn/guides/run/) 创建执行、查询状态和读取有界日志。
- [Workspace](/zh-cn/guides/workspaces/) 在旧写入者完成清理后，从新 Run 继续使用保留文件。
- [SDK](/zh-cn/sdk/) 提供 Go、Python 同步／异步和 TypeScript 接口。

不选择 Workspace 时，可写文件属于当前 Allocation。封存日志和声明输出在清理开始 15 分钟后过期。Workspace 有独立容量和删除语义；受支持的备份、升级与替换主机恢复仍未完成验收。
