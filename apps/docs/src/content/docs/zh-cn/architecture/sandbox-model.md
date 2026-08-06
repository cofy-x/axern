---
title: Sandbox 模型
description: 每个 SDK Sandbox 背后的共同心智模型——Service 承载的生命周期、执行、文件和显式清理。
---

Sandbox 是 SDK 对隔离工作负载的可编程视图。Python、Go、TypeScript 三个 SDK 基于同一套版本化契约实现同一模型，概念可以跨语言迁移。

## Sandbox 由 Service 承载

构造并启动一个 Sandbox 会编译为公开的控制面 API，而不是私有通道：

1. 解析或创建一个 **Environment**（镜像、Catalog 模板或已有环境 ID——严格三选一）。
2. 创建一个单副本 **Service** 并等待就绪的 Allocation。
3. 通过节点数据面执行命令、传输文件和开启隧道。

由于底层就是普通的 Axern 资源，它们对 CLI 可见（`axern service list`），并遵循与其他工作负载相同的命名空间配额、准入和授权规则。

## Source 与连接

每个 Sandbox 严格选择一个 source：便携的 OCI `image`、Catalog `template_id`，或用于继续已有工作的 `environment_id`。

连接是显式的。`AxernClient.from_env()` 读取 `AXERN_ENDPOINT` 和 `AXERN_TLS_*` 变量；`from_context()` 读取与 CLI 相同的版本化 Context schema。SDK 构造函数从不隐式检查用户目录。

## 执行与文件

启动后，Sandbox 支持：

- `exec()` 执行一次性命令并捕获退出码和输出；`process()` 用于交互式 stdin、终止和显式等待
- 文件操作（`read_text`/`write_text`、`list_dir`、`stat`、`mkdir`、`remove`、`copy`、`move`、`chmod`），以及基于归档、拒绝不安全路径和链接的 `upload_dir`/`download_dir`
- 由 SDK 负责续期和清理的反向[隧道](/zh-cn/guides/tunnels/)
- 在具备能力的镜像上进行 [Computer Use 和浏览器自动化](/zh-cn/guides/computer-use/)
- 创建时挂载的持久[卷](/zh-cn/guides/storage/)

## 生命周期与清理

`close()` 是刻意且有序的：停止隧道续期、吊销隧道会话、删除 SDK 创建的 Service（随之释放 Allocation），并且只在 Environment 由 SDK 创建时删除它——按 ID 传入的 Environment 永远不会被删除。所有清理都是尽力而为；优先用 Context Manager 或 `defer`，保证每条路径都执行清理。

Sandbox 的超时参数管的是就绪和 RPC 截止时间，不是存活期。Sandbox 没有内置的空闲过期；它的 Allocation 一直存在，直到 `close()` 或运维操作。

## 错误

公开 RPC 错误保留操作、RPC 错误码、服务端细节、可重试性和 Allocation 身份。校验、未找到、权限、超时、取消和不可用故障保持区分。SDK 从不隐式重试变更类 RPC；幂等读取和 Service watch 重连只在调用方的总截止时间内重试。

权威约定是仓库的 [SDK 用户模型](https://github.com/cofy-x/axern/blob/main/docs/product/sdk-user-model.md)。
