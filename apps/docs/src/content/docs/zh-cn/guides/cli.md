---
title: Axern CLI
description: 通过 Axern 公开 API 管理 Context 并运行隔离工作负载。
---

`axern` 是面向资源和交互式开发的产品 CLI。它通过 `gatewayd` 访问公开 API，从不直连节点或数据库内部。

![Axern CLI 命令概览](/terminal/axern.gif)

## 确认当前 Context

```bash
axern context list
axern context current
```

本机场景推荐的路径会为你创建并管理 Context：

```bash
axern local up
```

Helm 安装则需要保持 Gateway port-forward，并导入 Chart 生成的 mTLS 身份。SSH 是可选能力且默认 Chart 关闭，因此基础 CLI 路径只需转发控制和 HTTP 端口：

```bash
kubectl --namespace axern-system port-forward svc/gatewayd \
  25100:25000 25101:25080

axern context import-kubernetes local \
  --namespace axern-system \
  --endpoint 127.0.0.1:25100 \
  --service-url http://127.0.0.1:25101 \
  --ssh-endpoint "" \
  --current
```

交互式 Agent 工作流需要单独启用的 SSH 端口时，参考 [Kubernetes 安装指南](/zh-cn/getting-started/kubernetes/)。

## 诊断平台

从只读的平台 doctor 开始。它校验所选 Context、mTLS 证书有效期与密钥权限、Gateway 连通性、命名空间访问和运行时 Catalog，不创建任何资源：

```bash
axern doctor --namespace default
```

控制面可达性还不够时，使用显式探测：

```bash
axern doctor --namespace default --probe
```

探测会用 `python311` 模板创建临时的 Catalog 环境，执行一个小的 `runsc` Run，并在 Run 到达终态后删除该环境；Run 会作为正常的控制面历史保留。JSON 输出暴露稳定的检查码，且不打印证书路径、私钥、原始 endpoint 或服务端错误文本。Doctor 退出码：`0` 健康，`1` 降级，`2` 用法或连接配置无效，`3` 必需的平台健康检查失败。

用 `axern identity whoami` 查看所选 Context 的 Principal、当前证书和生效角色。平台管理员可用 `axern admin principal`、`axern admin credential` 和 `axern admin role-binding` 管理持久 Principal 和命名空间绑定。最小权限工作流见[身份与命名空间访问](/zh-cn/guides/authorization/)。

## 运行隔离的 Python

```bash
axern run docker.io/library/python:3.12-slim -- \
  python -c 'import platform; print(platform.python_version())'
```

定义需要评审或复用时，使用严格的资源文件：

```yaml title="run.yaml"
api_version: axern/v1
kind: Run
metadata:
  namespace: default
  labels:
    example: docs
spec:
  source:
    image: docker.io/library/python:3.12-slim
  command:
    argv: [python, -c, "print('ok')"]
  runtime_class: runsc
  resources: {}
```

```bash
axern run --file run.yaml
```

OCI 镜像是新工作负载的便携默认选择。当平台提供带策展工具链或配置的命名环境时，使用 `axern catalog list` 和 `--template`。

默认情况下 `run` 挂接 stdout/stderr，并以远端命令的退出码退出。用 `--detach` 异步创建，然后用 `axern run get`、`axern run list` 或 `axern run logs --follow` 查看。完整生命周期、Spec 字段和输出保留说明见 [Run 指南](/zh-cn/guides/run/)。

## 不把凭据写进 argv

Provider token 和不透明 Secret 优先走 stdin，避免值出现在命令参数中。引用已有环境变量也能避免把值写进命令或 heredoc 历史，但变量对本地 CLI 进程仍可见：

```bash
printf '%s\n' "$OPENAI_API_KEY" | axern agent profile set dev-codex \
  --agent codex \
  --provider openai \
  --upstream https://api.openai.com/v1 \
  --token-stdin \
  --model <model>

printf '%s\n' "API_KEY=$AXERN_SECRET_API_KEY" | \
  axern secret create --namespace default --literal-stdin
```

Agent profile 也可用 `--token-env NAME`。CLI 有意不接受以命令行参数传入 provider token 或不透明 Secret 值。

CLI help 是完整 flag 能力面的权威说明。Context、退出码、别名、Service、Tunnel 和 admin 工作流见仓库的 [CLI 源码指南](https://github.com/cofy-x/axern/tree/main/apps/cli)。
