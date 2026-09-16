---
title: 升级与版本
description: 让 CLI、本地栈、Helm Chart 和 SDK 保持在同一个连贯的 Axern 版本上。
---

Axern 的 CLI、Helm Chart、运行时镜像和三个 SDK 以同一仓库版本发布。1.0 前的 Release 应视为一个整体：混用版本不是受支持的组合。

## v0.7.0 全新状态边界

从 v0.6.2 升级至 v0.7.0 需要全新的控制面和节点本地状态，不能直接原地 Helm upgrade。替换状态前先导出所需输出、停止工作负载并确认清理完成。按照 [Kubernetes 安装指南](/zh-cn/getting-started/kubernetes/) 重新完成 Node 注册和 Principal Credential 配置。删除的 API、组件版本配套及回滚要求见 [v0.7.0 发布说明](https://github.com/cofy-x/axern/blob/main/docs/releases/v0.7.0.md)。不要在 sandbox 仍运行时删除恢复记录，也不要在发布完成前使用候选版本。

## 升级 CLI

使用 Homebrew：

```bash
brew upgrade axern
```

使用 shell 安装器时，重新执行并显式指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/cofy-x/axern/main/install.sh \
  | AXERN_VERSION=<version> sh
```

## 替换 Local Axern

CLI 不会静默改变运行中的本地栈，也不为本地数据库提供跨版本迁移兼容。先导出需要保留的输出，再显式重建：

```bash
axern local reset --force
axern local up
```

该操作会替换本地数据和身份材料；完整生命周期见 [Local Axern 参考](/zh-cn/guides/local/)。

## 升级 Kubernetes 安装

仅对明确支持保留现有状态的 Release，把 Chart 锁定到与 CLI 相同的版本，并根据新 Chart 审查运维自持的 values。不要沿用旧镜像标签或已删除的配置。以下命令不是从 v0.6.2 升级至 v0.7.0 的操作流程：

```bash
helm upgrade axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  -f values.yaml \
  --wait \
  --timeout 15m
```

Chart 的默认镜像是同一 Release 的不可变版本 Tag。确保 values 中覆盖的镜像也选择该版本。

## 锁定 SDK

让 SDK 包版本与 Axern Release 匹配，并提交 lockfile 或解析后的 `go.mod` 以保证可复现构建：

- Python：`uv add axern-sdk==<version>`
- Go：`go get github.com/cofy-x/axern/sdk/go@<version>`
- TypeScript：`pnpm add @cofy-x/axern-sdk@<version>`

生产环境不要使用浮动的 `latest` 依赖；SDK 与控制面契约是一起版本化的。

## 1.0 前的预期

1.0 之前，小版本可能变更公开命令、Spec 和 SDK 接口。跨小版本升级时阅读仓库 [`docs/releases/`](https://github.com/cofy-x/axern/tree/main/docs/releases) 中的策展发布说明，并在恢复工作负载前对升级后的平台运行 `axern doctor --namespace default`。
