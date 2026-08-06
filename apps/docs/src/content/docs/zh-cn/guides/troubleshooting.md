---
title: 故障排查
description: 用 doctor 家族和稳定退出码诊断 Context、本地栈、工作负载和隧道。
---

Axern 的诊断手段是只读的 doctor 家族加上可查询的资源状态。从与症状匹配的层级开始，逐层向下排查。

## 平台连通性与身份

```bash
axern doctor --namespace default
```

平台 doctor 校验所选 Context、mTLS 证书有效期与密钥权限、Gateway 连通性、命名空间访问和运行时 Catalog，不创建任何资源。退出码稳定，可用于自动化：`0` 健康，`1` 降级（如证书即将到期等警告），`2` 用法或连接配置无效，`3` 必需的平台检查失败。

仅可达性不够时，运行实时探测——它会创建临时的 Catalog 环境、执行一个小的 `runsc` Run，然后清理：

```bash
axern doctor --namespace default --probe
```

当检查报告授权失败时，用 `axern identity whoami` 确认 Principal、证书和生效角色。

## Local Axern

```bash
axern local status
axern local doctor
axern local logs gatewayd --follow --tail 100
```

`local doctor` 检查主机、Docker、端口、版本和组件健康，并为每个失败项给出可执行的修复建议。常见原因：端口冲突（本地栈不会分配替代端口）、Docker 代理配置，以及主机 resolver 文件未列出的 VPN DNS——设置 `AXERN_LOCAL_DNS_NAMESERVERS` 后重建本地栈。端口表和恢复流程见 [`axern local` 参考](/zh-cn/guides/local/)。

## 工作负载

一直无法就绪的 Run 或 Service，通常失败在 source 解析、准入或就绪探针：

```bash
axern run get <run-id> --output json
axern service get <service-id> --output json
axern service events <service-id>
axern quota get --namespace default
```

准入拒绝会携带诊断码和摘要；调整工作负载规模前先检查命名空间配额。Service 滚动更新通过 `axern service replicas <service-id>` 暴露 `updated` 与 `outdated` 副本。

## 隧道

```bash
axern tunnel doctor --session-id <session-id>
axern tunnel doctor --service-id <service-id> --local 127.0.0.1:8080
axern tunnel inspect <session-id>
axern tunnel events <session-id>
```

Tunnel doctor 校验会话、绑定和（配合 `--local`）本地上游，发现问题时以非零退出。会话生命周期语义见[反向隧道](/zh-cn/guides/tunnels/)。

## Agent Workspace

```bash
axern agent doctor
```

Agent doctor 诊断 Agent Profile：校验 Profile 配置，然后探测 Provider 上游兼容性和平台可达性，依赖不健康时以非零退出。见 [编码 Agent](/zh-cn/guides/agent/)。

状态和 doctor 类命令支持 `--output json`，可用于自动化。如果诊断全部通过但行为仍不正确，提交 issue 前请收集命令、其 JSON 输出和相关资源 ID。
