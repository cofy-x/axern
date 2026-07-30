---
title: Compose 快速开始
description: 在十分钟内启动完整 Axern 服务并运行一个沙箱。
---

官方 Release 路径会从已发布制品启动 PostgreSQL、MinIO、控制面、Gateway、节点服务和一个 Smoke 工作负载。

## 启动 Axern

```bash
git clone https://github.com/cofy-x/axern.git
cd axern
make quickstart
```

`make quickstart` 会等待服务就绪，并通过公开 Gateway 执行核心 Smoke。生成的 CLI 与 Context 保存在 `deploy/local/state/`。

## 运行沙箱

```bash
AXERN_CLI=deploy/local/state/releases/v$(cat VERSION)/axern

"${AXERN_CLI}" context current
"${AXERN_CLI}" run create \
  --image-ref docker.io/library/python:3.12-slim \
  --runtime-class runsc \
  --argv python \
  --argv -c \
  --argv 'print("hello from Axern")' \
  --wait
```

自动化场景使用 `--output json`。正常结束的 `run create --wait` 会返回工作负载本身的退出码。

## 检查与清理

```bash
make local-compose-status
make local-compose-purge
```

源码开发路径使用当前 checkout 构建本地 `:dev` 镜像：

```bash
make quickstart-source
```
