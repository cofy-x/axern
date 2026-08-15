---
title: Local Axern
description: 管理无需源码、机器级的 Axern 本地实例。
---

`axern local up` 是官方支持的本地安装入口。CLI 内置同版本部署资源，负责生成本地身份、调用 Docker Compose v2，并配置公开 Gateway Context。

它是机器级实例：可以从任意目录运行，不需要项目初始化文件。

## 启动和查看状态

```bash
axern local up
axern local image load python:3.12-slim --pull
axern local status
```

重复执行是幂等的，不会删除 PostgreSQL、对象、运行时和身份数据。如果已有其他当前 Context，`local up` 会保留它；使用 `--use` 显式切换。

可观测组件默认关闭，需要时显式启用：

```bash
axern local up --profile observability
```

## 诊断和日志

```bash
axern local doctor
axern local logs
axern local logs gatewayd --follow --tail 100
```

`doctor` 只读执行，并为每个失败项给出可操作的修复建议。自动化场景可对 `status` 或 `doctor` 使用 `--output json`。

## 停止、升级和删除

```bash
axern local down
axern local upgrade
axern local reset
```

`down` 删除容器和网络但保留数据。升级必须显式执行，并在迁移前创建本地备份。`reset` 永久删除实例，交互模式要求确认，CI 中必须使用 `--force`。

```bash
axern local path
```

完整端口、路径、代理和故障恢复说明见 [`axern local` 参考](/zh-cn/guides/local/)。

仓库 Compose 脚本和源码构建的 `:dev` 镜像仅用于贡献者开发，不是用户安装入口。
