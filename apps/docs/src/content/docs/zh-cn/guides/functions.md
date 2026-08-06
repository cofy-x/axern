---
title: Function
description: 部署具名 Python 函数，具备不可变版本、热 Worker 和调用历史。
---

Function 是 Axern 面向重复事件处理的模型。一个 Function 拥有稳定名称、不可变版本、Worker 伸缩和持久的调用历史。交互式的进程、文件、浏览器和 Computer Use 工作流请改用 Sandbox。

## 定义函数

Function Spec 在 `spec.source` 选择 Worker 环境，并把本地源码目录打包为不可变的函数 Bundle：

```yaml
api_version: axern/v1
kind: Function
metadata:
  name: hello
  namespace: default
spec:
  source:
    template: python311
  env:
    GREETING: hello
  function:
    runtime: python3.11
    handler: handler.hello
    initializer: handler.init
    source: src
    timeout_seconds: 600
    scaling:
      min_replicas: 0
      max_replicas: 10
      concurrency: 2
      idle_timeout: 5m
```

Handler 接收调用事件和一个包含环境、请求 ID 和初始化器状态的 Context：

```python
def init(context):
    return {"initialized": True, "function": context.function_name}


def hello(event, context):
    name = event.get("name", "world")
    greeting = context.env.get("GREETING", "hello")
    return {
        "message": f"{greeting} {name}",
        "request_id": context.request_id,
        "state": context.state,
    }
```

解析器会拒绝未知字段、冲突的 source、不安全的源码路径和非法的伸缩配置。凭据按 ID 引用，绝不写入 Spec。

Manifest 相对其所在目录解析 `function.source`。最小仓库结构为：

```text
hello/
  function.yaml
  payload.json
  src/
    handler.py
```

当 `source: src` 且 `handler: handler.hello` 时，Bundle 必须包含 `src/handler.py`。`payload.json` 放在 Bundle 之外，它是调用输入。Handler 的返回值会序列化为调用结果，抛出异常则产生失败的调用。初始化器状态会被热 Worker 复用，不能当作持久存储。

## 部署和调用

```bash
axern function deploy --file function.yaml --wait
axern function get --namespace default hello

axern function invoke --namespace default hello -d '{"name":"axern"}'
axern function invoke --namespace default hello --payload-file payload.json
axern function invoke --namespace default hello -d '{"name":"async"}' --async

axern function invocation list --namespace default hello
axern function invocation get <invocation-id>
axern function delete --namespace default hello
```

异步调用会持久排队，独立于客户端连接到达终态。Dispatcher 故障后的投递是至少一次，因此 Handler 应使用稳定的调用 ID 对外部副作用去重。版本替换和删除会等待进行中的调用完成。

## 从 Python 部署

```python
from axern_sdk import AxernClient, Function

client = AxernClient.from_context("~/.config/axern/config.json")
fn = Function.from_file(client, "function.yaml")

fn.deploy(wait_ready=True)
result = fn.invoke({"name": "axern"})
print(result.value)   # handler return value
print(result.status)  # "succeeded"
```

`Function.deploy()` 把源码打包为确定性 Bundle、上传并创建版本；`invoke()` 使用专用调用 API，而不是通用 Run 封装。

完整源码目录见仓库的 [function-hello 示例](https://github.com/cofy-x/axern/tree/main/examples/function-hello)；[Function 用户模型](https://github.com/cofy-x/axern/blob/main/docs/product/function-user-model.md) 是权威约定。
