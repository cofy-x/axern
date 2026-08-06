---
title: 身份与命名空间访问
description: 理解 Axern Principal、作用域角色、证书轮换和最小权限的托管执行。
---

Axern 把每个通过验证的客户端证书映射为持久的 Principal。公开的 CLI 和 SDK 调用经 `gatewayd` 进入；控制面为每个平台或命名空间操作做 Principal 授权。

先查看所选 Context：

```bash
axern identity whoami
axern doctor --namespace default
```

内置角色是 `platform_admin`、`namespace_admin`、`namespace_editor` 和 `namespace_viewer`。命名空间角色只作用于一个命名空间。Viewer 可以查看资源，Editor 还能创建和执行工作负载，命名空间管理员可以管理该命名空间的角色绑定。平台管理员管理 Principal、凭据和平台级操作。

## 把引导管理与日常访问分开

本页的命令需要已有的平台管理员 Context。开发者不应使用自己的 namespace-editor Context 创建 Principal 或授予角色。运维者应引导一个短期管理员 Context，创建应用 Principal 和证书，然后切回最小权限 Context 进行日常工作。

## 添加命名空间 Editor

创建 Principal、注册其公钥证书并绑定角色：

```bash
axern admin principal create developer \
  --display-name "Developer" \
  --kind human

axern admin credential add <principal-id> \
  --certificate developer.crt \
  --label laptop

axern admin role-binding grant \
  --principal-id <principal-id> \
  --scope namespace \
  --namespace default \
  --role namespace_editor
```

把匹配的私钥和证书注册为独立的本地 Context；私钥从不上传：

```bash
axern context set developer \
  --endpoint <gateway-host:port> \
  --service-url <gateway-http-url> \
  --tls-ca-cert ca.crt \
  --tls-cert developer.crt \
  --tls-key developer.key \
  --proxy-mode direct \
  --current

axern identity whoami
axern doctor --namespace default
```

证书签发和 CA 策略仍由运维者掌管；证书必须由 Gateway 信任的 CA 签发，且注册的公钥证书必须匹配 `developer.key`。

私钥保留在用户的 Context 中，从不上传。轮换证书时，先添加新的公钥证书，切换客户端 Context，确认 `identity whoami`，然后吊销旧凭据。

托管的 Axrun Worker 使用专用服务身份。其命名空间访问还受当前执行的 Rollout 工作的短期持久租约进一步限制；Worker 证书本身不能编辑用户命名空间。

完整的信任与审计模型见仓库的 [授权架构](https://github.com/cofy-x/axern/blob/main/docs/architecture/authorization.md)。运行审计化管理工作流（节点退役、Service 清理、存储回收、审计查看）的集群运维者，应遵循仓库的 [Dashboard 与 admin 运维指南](https://github.com/cofy-x/axern/blob/main/apps/cli/docs/dashboard-admin-operations.md)；这些 Runbook 随 CLI 维护，不在此复制。
