---
title: SSH 访问
description: 打开进入 Allocation 或就绪服务副本的 SSH 兼容终端。
---

`axern ssh` 通过 Gateway 的 SSH 边缘，打开进入运行中 Allocation 或就绪服务副本的交互式终端。它使用本地 OpenSSH 客户端，以及所选 Context 中的 SSH endpoint 和身份文件。

:::caution[SSH 是显式的信任边界]

默认 Helm 安装禁用 SSH。仅在交互式工作流需要时启用，配置受信公钥，并限制私有身份文件权限。SSH 使用自己独立的 Gateway 级 `authorized_keys` 边界，不继承 Principal 或命名空间 RBAC。任何持有受信身份的人都可以对 Gateway 能解析的任意 Allocation ID 请求 shell。

:::

```bash
axern ssh <allocation-id>
axern ssh <service-id>
```

目标是 Service 时，CLI 选择一个就绪副本；传 `--allocation-id` 指定具体副本。在目标后追加命令可执行一次性命令，而不是进入交互式 shell：

```bash
axern ssh <service-id> -- uname -a
axern ssh <allocation-id> --shell /bin/sh
```

常用 flag：

- `--user <name>`：会话使用的容器用户。
- `--identity-file <path>` / `--ssh-endpoint <host:port>`：覆盖 Context 连接配置，也可用 `AXERN_SSH_IDENTITY_FILE` 和 `AXERN_SSH_ENDPOINT` 配置。
- `--ssh-option <option>`：透传额外的 OpenSSH 选项，可重复。
- `--strict-host-key-checking`：强制执行本地 `known_hosts` 策略。默认对临时 Sandbox 主机放宽检查；共享部署应使用严格检查，并在信任新 Gateway 前审视 host-key 轮换。

需要预装 Agent 的持久编码 Workspace，优先用 [`axern agent shell`](/zh-cn/guides/agent/)；需要从 Allocation 内部访问本地 TCP 服务，用[反向隧道](/zh-cn/guides/tunnels/)。
