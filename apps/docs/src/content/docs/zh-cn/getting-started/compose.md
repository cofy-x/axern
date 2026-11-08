---
title: Compose 开发
description: 管理无需源码的 Docker Compose 开发实例。
---

`axern dev up` 管理 Docker Compose 开发配置。CLI 内置同版本部署资源，负责生成本地身份、调用 Docker Compose v2，并配置公开 Gateway Context。

它是机器级实例：可以从任意目录运行，不需要项目初始化文件。

## 启动和查看状态

```bash
axern dev up
axern dev image load python:3.12-slim --pull
axern dev status
```

重复执行是幂等的，不会删除 PostgreSQL、对象、运行时和身份数据。如果已有其他当前 Context，`dev up` 会保留它；使用 `--use` 显式切换。

可观测组件默认关闭，需要时显式启用：

```bash
axern dev up --profile observability
```

## 诊断和日志

```bash
axern dev doctor
axern dev logs
axern dev logs gatewayd --follow --tail 100
```

`doctor` 只读执行，并为每个失败项给出可操作的修复建议。自动化场景可对 `status` 或 `doctor` 使用 `--output json`。

## 停止或重建

```bash
axern dev down
axern dev reset --force
axern dev up
```

`down` 删除容器和网络但保留数据。本地状态跨版本只支持重建：先导出需要保留的输出，再执行 `reset --force` 和 `up`。不带 `--force` 的 `reset` 会在永久删除实例前要求交互确认。

```bash
axern dev path
```

完整端口、路径、代理和故障恢复说明见 [`axern dev` 参考](/zh-cn/guides/local/)。

仓库 Compose 脚本和源码构建的 `:dev` 镜像仅用于贡献者开发，不是用户安装入口。
