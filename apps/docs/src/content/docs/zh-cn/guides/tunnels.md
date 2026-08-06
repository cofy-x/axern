---
title: 反向隧道
description: 把本地 TCP 服务暴露给 Axern 服务或 Sandbox 中运行的代码。
---

Axern 隧道是反向 TCP 隧道：远端 Allocation 中的代码访问 Axern 在 Allocation 内绑定的 localhost 端口，流量被转发回你工作站上的 TCP 目标。适用于远端工作负载需要访问开发 API 服务、Mock 或持有凭据的本地代理的场景。它与 port-forward 相反：你的机器不通过隧道调用远端服务。

:::caution[本地网络暴露]

远端 Allocation 中的每个进程都能访问隧道的本地目标。不要把隧道指向生产数据库、云元数据端点或管理接口。本地开发服务绑定 loopback，使用短生命周期会话，并在隧道存续期间把远端工作负载视为可信。隧道不会让本地目标暴露到公网，但它按设计跨越了 Sandbox 边界。

:::

## 从 Service 开隧道

先启动本地目标，再从就绪的服务副本开启前台隧道：

```bash
python3 -m http.server 8080 --bind 127.0.0.1

axern service create --template-id python311 --replicas 1
axern service tunnel <service-id> --to 127.0.0.1:8080
```

命令会选择一个稳定的就绪副本、创建隧道会话、等待 Allocation 本地绑定完成，并打印会话与绑定地址：

```text
Service: svc-...
Selected allocation: alloc-...
Tunnel session: tun-...
Local target: 127.0.0.1:8080
Remote bind: 127.0.0.1:42377
Press Ctrl-C to revoke the tunnel.
```

此后在 Allocation 内 `curl http://127.0.0.1:42377/` 即可到达你本地的 `127.0.0.1:8080`。用 `--allocation-id` 或 `--node-id` 指定副本。远端工作负载需要本地目标期间保持命令运行；Ctrl-C 吊销会话。

`axern tunnel open --allocation-id <allocation-id> --local 127.0.0.1:8080` 是更底层、按 Allocation 作用的调试入口。

## 从 SDK Sandbox 开隧道

Python SDK 掌管连接器，并在 Sandbox 活跃期间自动续期隧道 TTL：

```python
from axern_sdk import AxernClient, Sandbox

client = AxernClient.from_context("~/.config/axern/config.json")

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    upstream="127.0.0.1:8080",
    remote_port=8786,
) as sandbox:
    print(sandbox.bound_addr)
```

## 诊断和清理

```bash
axern tunnel list --allocation-id <allocation-id>
axern tunnel inspect <session-id>
axern tunnel doctor --service-id <service-id> --local 127.0.0.1:8080
axern tunnel revoke <session-id> --reason manual-cleanup
```

Doctor 检查控制面状态、Gateway 中继可达性、最近的对端事件和本地上游探测。其 JSON 输出有意排除隧道 token。中继连接走 Gateway 控制边缘的 mTLS 路径，因此开发和生产 Context 使用同一个公开入口模型。

完整的会话生命周期和中继路径见仓库的 [隧道文档](https://github.com/cofy-x/axern/blob/main/apps/cli/docs/tunnel.md)。
