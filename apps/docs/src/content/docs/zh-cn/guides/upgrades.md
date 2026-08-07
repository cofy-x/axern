---
title: 升级与版本
description: 让 CLI、本地栈、Helm Chart 和 SDK 保持在同一个连贯的 Axern 版本上。
---

Axern 的 CLI、Helm Chart、运行时镜像和三个 SDK 以同一仓库版本发布。1.0 前的 Release 应视为一个整体：混用版本不是受支持的组合。

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

## 升级 Local Axern

CLI 不会静默改变运行中的本地栈。当 `axern local status` 报告版本不匹配时，显式迁移：

```bash
axern local upgrade
```

升级会停止旧栈，对数据、身份、元数据和部署文件创建带时间戳的备份，应用受支持的迁移，并验证健康状态。降级会被拒绝；完整生命周期见 [Local Axern 参考](/zh-cn/guides/local/)。

## 升级 Kubernetes 安装

把 Chart 锁定到与 CLI 相同的 Release，并复用你的 values：

```bash
helm upgrade axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  -f values.yaml \
  --reuse-values \
  --wait \
  --timeout 15m
```

Chart 的默认镜像是同一 Release 的不可变版本 Tag，Chart 与工作负载一起移动。

## 锁定 SDK

让 SDK 包版本与 Axern Release 匹配，并提交 lockfile 或解析后的 `go.mod` 以保证可复现构建：

- Python：`uv add axern-sdk==<version>`
- Go：`go get github.com/cofy-x/axern/sdk/go@<version>`
- TypeScript：`pnpm add @cofy-x/axern-sdk@<version>`

生产环境不要使用浮动的 `latest` 依赖；SDK 与控制面契约是一起版本化的。

## 1.0 前的预期

1.0 之前，小版本可能变更公开命令、Spec 和 SDK 接口。跨小版本升级时阅读仓库 [`docs/releases/`](https://github.com/cofy-x/axern/tree/main/docs/releases) 中的策展发布说明，并在恢复工作负载前对升级后的平台运行 `axern doctor --namespace default`。
