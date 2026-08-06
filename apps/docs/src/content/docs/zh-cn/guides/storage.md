---
title: 存储与卷
description: 通过 Axern 的 Storage V1 模型为服务和 Sandbox 挂载持久卷。
---

Axern 存储把控制面意图与节点本地的发布工作分离。`storaged` 掌管卷类、Claim、Allocation 绑定和回收策略；`volumed` 在被选中的节点上把发布的卷实体化。Claim 描述数据意图和存活期；绑定描述一次 Allocation 的挂接，在一个 Claim 的生命周期内可以被创建、发布、释放和重试多次。

当前的 Provider 是 `local`，由卷节点上的受管目录承载。本地卷把恢复绑定在其原始节点上；节点缺失是显式的存储拓扑故障，而不是被静默替换为空目录。

:::caution[高级 API 状态]

Storage V1 暴露了控制面模型和挂载意图，但稳定版 `axern` CLI 尚未提供面向用户的 VolumeClass/VolumeClaim 创建、列表、删除工作流。因此下面的 `VolumeMount` 示例不是独立的卷开通教程：它假定具名 Claim 已由平台托管的工作流创建，例如 Agent Workspace 或服务持有的 Claim。

:::

## 从 Python 挂接已有卷

用 `VolumeMount` 把 Service V1 卷挂到 Service 承载的 Sandbox 上：

```python
from axern_sdk import AxernClient, Sandbox, VolumeMount

client = AxernClient.from_context("~/.config/axern/config.json")

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    volumes=[
        VolumeMount("data", "/data"),
        VolumeMount("cache", "/cache", readonly=True, options=("rbind",)),
    ],
) as sandbox:
    result = sandbox.exec("ls /data /cache", text=True, check=True)
    print(result.stdout)
```

SDK 通过公开控制面传递卷意图；存储解析、节点发布、挂载注入和释放仍由平台掌管。`VolumeMount("data", "/data")` 不会创建缺失的 Claim。

## 卷出现在哪里

- **编码 Agent Workspace** 把持久卷挂载在 `/home/axern/workspace`；数据在 `agent stop` 后保留，并随下一次会话恢复。见 [编码 Agent](/zh-cn/guides/agent/)。
- **Service** 在其 Claim 存在期间，跨 Allocation 替换和节点运行时重启保留服务级数据。

## 持久性与恢复

在当前的 `local` Provider 下，数据能跨 Allocation 替换和节点运行时重启存活，但仍绑定在其原始节点上。Axern 不会自动复制、故障转移或备份本地卷。节点缺失是显式的存储拓扑故障，而不是被替换为空目录。

## 回收与运维可见性

释放 Allocation 会移除其绑定，但不删除 Claim 及其数据。回收策略只在 Claim 被删除时评估。`Retain` 保留后端数据并留下可审计的墓碑；`Delete` 在节点确认物理删除后才完成。

用 `axern admin storage list` 查看绑定，`axern admin storage reclaim list` 查看待物理删除项，`axern admin reliability check` 查看平台级存储健康。

职责、生命周期和回收约定见仓库的 [存储架构](https://github.com/cofy-x/axern/blob/main/docs/architecture/storage-architecture.md)。
