---
title: 持久 Workspace
description: 在一台 Linux 主机上保留文件，并从新的隔离执行实例继续工作。
---

Workspace 是具有独立身份、Namespace、容量和删除语义的保留数据。一个 Run 可以在 `/workspace` 挂载一个 Workspace，同一时刻只允许一个 Allocation 写入。镜像提供可复现的工具，Agent 任务和会话由上层应用管理。

:::caution[实验性存储]

CLI、SDK 和服务端必须使用一致的构建版本。原生主机需要明确配置 XFS 项目配额预算。当前功能支持清理旧执行后，在新执行中继续使用文件；尚不提供受支持的备份、升级或替换主机恢复。源码版本不代表安装包已经发布。

:::

## 准备主机

按[原生安装指南](https://github.com/cofy-x/axern/blob/main/deploy/standalone/INSTALL.md)配置独立 XFS 数据挂载、`prjquota`、有限的 Workspace 字节预算和 inode 预算。未启用或无法验证限制的 Node 会拒绝创建 Workspace。Compose 开发配置不会隐式开启持久存储。

`axern server` 管理当前 Linux 主机上的原生安装，`axern dev` 管理独立的 Compose 开发实例。API Context 选择 Workspace 和 Run 请求的服务端，不改变 `server` 管理的主机。

## 创建和选择数据

使用安装配置中已准入的 Node ID：

```bash
axern workspace create --node node-personal-01 --capacity 1Gi --inodes 100000
axern workspace list
axern workspace get <workspace-id>
axern run --workspace <workspace-id> --limit-memory 512Mi python:3.12-slim -- \
  python -c 'from pathlib import Path; Path("/workspace/note.txt").write_text("retained work\n")'
```

创建默认等待数据就绪，`--wait=false` 返回初始观察状态。Run 选定的 Workspace 不可变；替换执行会获得新的 Allocation ID 和临时根文件系统，同时保留所选 Workspace 的文件。

## 从新的 Sandbox 继续工作

Python 同步客户端与 `AsyncAxernClient` / `AsyncSandbox` 提供相同语义：

```python
from axern_sdk import AxernClient, Sandbox

client = AxernClient.from_context("/absolute/path/to/config.json")
workspace = client.create_workspace(
    node_id="node-personal-01", capacity_bytes=1024**3, inode_limit=100000,
)
client.wait_workspace_ready(workspace.id)

with Sandbox(client=client, image="python:3.12-slim", workspace_id=workspace.id, limit_memory="512Mi") as first:
    first.write_text("/workspace/note.txt", "retained work\n")

client.wait_workspace_ready(workspace.id)
with Sandbox(client=client, image="python:3.12-slim", workspace_id=workspace.id, limit_memory="512Mi") as second:
    assert second.read_text("/workspace/note.txt") == "retained work\n"

client.close()
```

关闭 Sandbox 请求取消执行，并保留 Workspace。`wait_workspace_ready` 等待数据就绪且旧挂载完成清理，只读取状态，不重试变更，也不预留下一个写入者。竞争请求仍可能先取得写入权。Go 的 `WaitWorkspaceReady` 和 TypeScript 的 `waitWorkspaceReady` 具有相同含义。

旧进程、文件、SSH、终端和 Tunnel 句柄始终绑定原来的 Allocation，不能访问替换实例。保留文件不代表恢复进程内存、Agent 对话或外部操作。

## 容量和删除

字节硬限制必须为正且按 512 字节对齐，inode 限制必须为正。CLI 支持 `1Gi` 等数量。数据在分离、不可用和删除中仍保留容量计费，与 Run 的临时根存储独立。项目配额统计实际文件系统分配；Guest 文件大小和稀疏文件统计不是计费权威。配额不保证其他宿主写入者不会耗尽物理磁盘。

Namespace 配额包含持久字节和 inode 限制，通过 `axern quota get` 查看，由管理员用 `axern quota set` 设置。存储缺失、身份变化、限制失效和磁盘耗尽都会明确失败，不会把缺失目录替换为空 Workspace。

```bash
axern workspace delete <workspace-id>
axern workspace get <workspace-id>
```

删除是显式、异步操作，活跃或未完成清理的挂载会阻止删除。只有确认数据清理完成并进入 `deleted` 后才释放计费；删除失败会保留计费和诊断。开发重置和手动删除目录不是 serving 维护流程。

当前只支持单写者，不提供并行只读挂载、任意宿主路径、扩容或共享多写。Workspace 不能与根文件系统发布同时使用；仍可通过有界 declared output 明确交接产物。
