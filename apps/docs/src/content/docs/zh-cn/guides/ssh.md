---
title: SSH 访问
description: 打开进入运行中 Allocation 的 SSH 兼容终端。
---

`axern ssh` 通过 Gateway 的 SSH 边缘，打开进入运行中 Allocation 的交互式终端。它使用本地 OpenSSH 客户端，以及所选 Context 中的 SSH endpoint 和身份文件。

:::caution[SSH 是显式的信任边界]

默认 Helm 安装禁用 SSH。需要交互式工作流时启用，并配置持久的 Gateway 主机密钥。使用 `axern admin credential add --ssh-public-key <file> --expires-at <RFC3339> <principal-id>` 将客户端公钥注册为具有明确有效期的 Principal Credential。现有命名空间角色必须允许对目标 Allocation 执行操作。限制私钥文件权限；活动会话会重新检查凭据撤销和命名空间权限变化，无法确认授权时关闭访问连接，但不取消 Allocation。

:::

```bash
axern ssh <allocation-id>
```

在目标后追加命令可执行一次性命令，而不是进入交互式 shell：

```bash
axern ssh <allocation-id> -- uname -a
axern ssh <allocation-id> --shell /bin/sh
```

常用 flag：

- `--user <name>`：会话使用的容器用户。
- `--identity-file <path>` / `--ssh-endpoint <host:port>`：覆盖 Context 连接配置，也可用 `AXERN_SSH_IDENTITY_FILE` 和 `AXERN_SSH_ENDPOINT` 配置。
- `--ssh-option <option>`：透传额外的 OpenSSH 选项，可重复。
- `--strict-host-key-checking`：强制执行本地 `known_hosts` 策略。默认对临时 Sandbox 主机放宽检查；共享部署应使用严格检查，并在信任新 Gateway 前审视 host-key 轮换。

需要从 Allocation 内部访问本地 TCP 服务时，使用[反向隧道](/zh-cn/guides/tunnels/)。
