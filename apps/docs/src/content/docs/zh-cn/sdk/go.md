---
title: Go SDK
description: 用 Go 创建和编程 Axern Sandbox。
---

Go API 接收 `context.Context`、返回类型化错误，并要求显式清理。

```bash
go get github.com/cofy-x/axern/sdk/go@<version>
```

把 `<version>` 替换为 Gateway 和运行时使用的 Axern Release；Go SDK 不是浮动的 `latest` 依赖。

官方 module 索引在 [`pkg.go.dev`](https://pkg.go.dev/github.com/cofy-x/axern/sdk/go)。

```go
ctx := context.Background()
client, err := axern.NewClient(ctx, "127.0.0.1:25000")
if err != nil {
    log.Fatal(err)
}
defer client.Close()

sandbox, err := axern.NewSandbox(axern.SandboxOptions{
    Client: client,
    Image: "docker.io/library/python:3.12-slim",
})
if err != nil {
    log.Fatal(err)
}
defer sandbox.Close(ctx)

if err := sandbox.Start(ctx); err != nil {
    log.Fatal(err)
}

result, err := sandbox.Exec(
    ctx,
    "python -c \"print('hello from Go')\"",
    axern.ExecOptions{Check: true},
)
if err != nil {
    log.Fatal(err)
}
fmt.Print(result.StdoutString())
```

SDK 创建的 Sandbox 优先用 `defer sandbox.Close(ctx)` 清理。用 `IsNotFound`、`IsTimeout`、`IsValidation` 等辅助函数做错误分支，不要解析错误文本。

- [Go SDK 源码与完整指南](https://github.com/cofy-x/axern/tree/main/sdk/go)
- [可运行的 Go 示例](https://github.com/cofy-x/axern/tree/main/sdk/go/examples)
