---
title: SDK
description: 选择 Python、Go 或 TypeScript 的 Axern SDK。
---

三个 SDK 暴露同一个可编程 Sandbox 边界：source、生命周期、执行、挂接进程、文件、归档传输、隧道、元数据和类型化错误。变更类 RPC 从不隐式重试。

:::caution[SDK 版本对齐 Axern Release]

SDK 与控制面契约一起版本化。请使用与 Axern CLI/Chart Release 匹配的包版本；生产环境不要使用浮动的 `latest` 依赖。

:::

| SDK | 适用场景 | 开始 |
| --- | --- | --- |
| Python | Agent 工具、Notebook、编排 | [Python SDK](/zh-cn/sdk/python/) |
| Go | 服务和基础设施控制器 | [Go SDK](/zh-cn/sdk/go/) |
| TypeScript | Node.js 应用和工具 | [TypeScript SDK](/zh-cn/sdk/typescript/) |

## 能力矩阵

Sandbox 边界是共享的；各语言的深度差异是刻意设计。

| 能力 | Python | Go | TypeScript |
| --- | --- | --- | --- |
| Sandbox 生命周期、exec、进程 | ✓ | ✓ | ✓ |
| 文件与归档传输 | ✓ | ✓ | ✓ |
| 反向隧道 | ✓ | ✓ | ✓ |
| Computer Use | ✓ | ✓ | ✓ |
| 托管浏览器 | ✓ | — | — |
| Volume | ✓ | ✓ | ✓ |
| Function（打包与调用） | ✓ | — | — |
| Environment 与 Service（创建、watch） | ✓ | ✓ | — |
| Rollout 控制与任务资产辅助 | — | ✓ | — |
| 并发模型 | 同步 + `AsyncSandbox` | `context.Context` | Promise 原生 |

Secret、配额、命名空间、SSH 和 admin 授权目前以 CLI 为产品界面；SDK 只在生成的 protobuf 层暴露它们。

从公开的包仓库安装官方包：

- Python：`uv add axern-sdk==<version>`，来自 [PyPI](https://pypi.org/project/axern-sdk/)
- Go：`go get github.com/cofy-x/axern/sdk/go@<version>`，来自 [Go module index](https://pkg.go.dev/github.com/cofy-x/axern/sdk/go)
- TypeScript：`pnpm add @cofy-x/axern-sdk@<version>`，来自 [npm](https://www.npmjs.com/package/@cofy-x/axern-sdk)

每个 Sandbox 严格选择一个 source：便携默认用 OCI 镜像，命名可复用环境用 Catalog 模板，继续已有工作用环境 ID。生命周期和数据面操作都走 Axern 公开 API；SDK 不会为平台行为增加 SSH 或 shell 旁路。

Axern 的 CLI、Helm Chart、运行时镜像和三个 SDK 以同一仓库版本发布。Python 和 TypeScript 使用公开包仓库；Go 使用带版本的 `sdk/go` module。1.0 前的 Axern Release 应视为一个整体，提交包管理器 lockfile 或解析后的 `go.mod` 版本以保证可复现构建。
