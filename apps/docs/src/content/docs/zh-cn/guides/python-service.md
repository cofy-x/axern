---
title: Python Service
description: 用 Python SDK 创建 HTTP 服务，并通过 Axern Gateway 访问它。
---

这个维护中的示例创建一个 Environment 和一个单副本 Service，等待就绪的 Allocation，通过公开的 `/svc` Gateway 路由访问其 HTTP 服务，最后删除两个资源。它需要 `uv` 和一个带 HTTP `service_url` 的可用 Axern Context；Compose 和 Kubernetes 快速开始都会为你创建该 Context 字段。Service 资源模型本身——探针、滚动更新和副本查看——见 [Service](/zh-cn/guides/service/)。

![通过 Axern 创建、访问并清理镜像承载的 Python Service](/terminal/python-service.gif)

```bash
# 从仓库根目录运行；使用检出目录中的 SDK 包。
uv run --package axern-sdk \
  python sdk/python/examples/service_gateway.py
```

使用已安装的 SDK 时，执行 `uv add axern-sdk==<version>`，并把同样的 `AxernClient.create_environment()` 和 `create_service()` 调用适配到你的应用。

示例从所选 Axern Context 读取 endpoint 和 mTLS 身份。它以便携的 Python OCI 镜像启动，可信的长驻工作负载使用 `runc`，响应 `Hello from Axern`，验证 Gateway 响应，等待 Service 删除完成，并做防御性清理。完整源码：[`sdk/python/examples/service_gateway.py`](https://github.com/cofy-x/axern/blob/main/sdk/python/examples/service_gateway.py)。

Gateway URL 的组装方式：

```text
<context.service_url>/svc/<namespace>/<service-id>/<container-port>/
```

示例监听 `0.0.0.0:8080`，因此在默认命名空间下路由为 `/svc/default/<service-id>/8080/`。如果所选 Context 没有 `service_url`，运行示例前把 `AXERN_SERVICE_URL` 设为 Gateway 的 HTTP base URL。

Service 活跃期间，CLI 读取同一份权威资源：

```bash
axern service get <service-id> --output json
axern service replicas <service-id>
axern service events <service-id>
```

Service 状态以 `controld` 为准；SDK 不选择节点，也不缓存路由。Gateway 解析和端点健康是平台职责。如果示例在清理前失败，用打印的资源 ID 检查状态后手动删除 Service 和 Environment。
