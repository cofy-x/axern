---
title: 编码 Agent
description: 在持久的 Axern Workspace 中运行 Codex 或 Claude Code，Provider 凭据留在本机。
---

`axern agent` 为 Codex 和 Claude Code 管理持久的远程编码 Workspace。Workspace 维持一个负责计算的 Service、一个挂载在 `/home/axern/workspace` 的项目数据 Volume，以及每个连接会话一个 Tunnel。Agent bundle 从控制面 Catalog 解析并只读挂载；默认的 `coding-base` 模板决定 Workspace rootfs，而不是 Agent 可执行文件。

Provider token 从不离开你的机器。远端运行时只拿到会话级的本地适配器 token 和 loopback base URL；真实上游凭据由本地凭据代理持有。

## 创建 Profile

Profile 保存在本地 CLI 配置的 `agent_profiles` 下。Codex 上游必须实现 OpenAI Responses API：

```bash
axern agent profile set dev-codex \
  --agent codex \
  --provider openai \
  --upstream https://api.openai.com/v1 \
  --token-stdin \
  --model <model> \
  --use

axern agent profile set dev-claude \
  --agent claude-code \
  --provider anthropic \
  --upstream https://api.anthropic.com \
  --token-env AXERN_ANTHROPIC_TOKEN \
  --model <model>
```

第一条命令从 stdin 读取一个 token；第二条读取指定的环境变量。例如：

```bash
printf '%s\n' "$OPENAI_API_KEY" | axern agent profile set dev-codex \
  --agent codex \
  --provider openai \
  --upstream https://api.openai.com/v1 \
  --token-stdin \
  --model <model> \
  --use
```

Profile 和 doctor 命令从不打印已存储的 token。CLI 只通过 stdin 或指定环境变量接受 Provider 凭据，不接受命令行参数。能用 stdin 时优先 stdin；环境变量可以让值不出现在 argv 中，引用已有变量则避免把值写进命令历史。该变量对本地 CLI 进程仍可见。

## 启动会话

```bash
axern agent doctor --workspace project-a --profile dev-codex
axern agent shell --workspace project-a --profile dev-codex
```

首次会话会自动创建 Workspace：启动或恢复 Service、等待就绪副本、开启凭据代理和 Tunnel，并写入远端 Agent 配置。省略 `--workspace` 时使用所选 Profile 名。

非交互地运行配置好的 Agent CLI，参数放在 `--` 之后：

```bash
axern agent run --workspace project-a --profile dev-codex -- exec --model <model> "reply ok only"
axern agent run --workspace project-a --profile dev-claude -- -p "reply ok only"
```

用 `connect` 保持代理、隧道和远端配置活跃，但不进入 shell。

## 挂起、切换和删除

结束会话只关闭该连接。挂起计算资源但保留 Workspace 数据：

```bash
axern agent list
axern agent stop --workspace project-a
```

`stop` 把 Service 缩容到零，且是幂等的。下一次 `shell`、`run` 或 `connect` 会恢复同一个 Service 和 Volume。运行中的 Workspace 只接受其当前 Profile；要切换 Agent 或模型，先挂起再用另一个 Profile 重连。

仅在不再需要数据时删除已挂起的 Workspace：

```bash
axern agent stop --workspace project-a
axern agent workspace delete --workspace project-a --yes
```

删除会等待 Allocation 释放和物理卷回收。用同名重建 Workspace 会得到新的身份和空数据目录。

:::note
`axern agent` 是交互式开发路径。需要轨迹和证据的可复现 Agent 执行请用 [Axrun](/zh-cn/axrun/)。
:::

完整的 Workspace、安全和诊断约定见仓库的 [Agent 运行时文档](https://github.com/cofy-x/axern/blob/main/apps/cli/docs/agent.md)。
