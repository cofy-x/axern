---
title: Secret
description: 存储不可变、列表仅暴露元数据的 Secret，用于仓库凭据和工作负载配置。
---

`axern secret` 在命名空间中存储不可变 Secret。其他资源按 ID 引用 Secret——例如环境的 `--registry-credential-id`——Secret 绝不写入资源 Spec。列表和查看只暴露元数据，不暴露 Secret 值。

## 创建 Secret

不透明 Secret 接受一个或多个 `key=value` 条目。优先用 stdin，避免值出现在命令参数中。引用已有环境变量，而不是把值写进命令或 heredoc：

```bash
printf '%s\n' "API_KEY=$AXERN_SECRET_API_KEY" | \
  axern secret create \
    --namespace default \
    --literal-stdin \
    --label team=runtime
```

stdin 格式每行一个 `KEY=VALUE` 条目。空行被忽略，值逐字保留，JSON 编码后的 `string_data` 映射不得超过 64 KiB。CLI 有意不接受以命令行参数传入不透明 Secret 值。

Docker 仓库凭据使用 `docker-config-json` 类型和本地配置文件：

```bash
axern secret create \
  --namespace default \
  --type docker-config-json \
  --file ~/.docker/config.json
```

## 查看和删除

```bash
axern secret list --namespace default
axern secret get <secret-id>
axern secret delete <secret-id>
```

Secret 是不可变的。轮换采用替换式工作流：

1. 用新值创建一个新 Secret。
2. 更新或替换所有引用旧 Secret ID 的资源。
3. 等待新的 Service、Function、Run 或 Environment 版本就绪并验证工作负载。
4. 确认没有活跃资源引用后，才删除旧 Secret。

轮换 Environment 的仓库凭据时需要创建新 Environment，因为 Environment 本身不可变，然后把工作负载指向新 Environment。`secret-env` 和 `secret-file` 投影则更新 Service 或 Function Spec，让其不可变版本滚动发布。

:::note
Secret 存放平台凭据材料，如镜像仓库拉取凭据。Agent Provider token 有专用存储，明文不经过通用 API：交互式 Workspace 用本地 `axern agent` Profile，托管 Rollout 用版本化的 [Axrun Profile](/zh-cn/axrun/)。
:::
