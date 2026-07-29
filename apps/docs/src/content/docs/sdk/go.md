---
title: Go SDK
description: Create and program an Axern sandbox from Go.
---

Go APIs take `context.Context`, return typed errors, and make cleanup explicit.

```bash
go get github.com/cofy-x/axern/sdk/go@latest
```

The official module is indexed on
[`pkg.go.dev`](https://pkg.go.dev/github.com/cofy-x/axern/sdk/go).

```go
ctx := context.Background()
client, err := axern.NewClient(ctx, "127.0.0.1:25000")
if err != nil {
    log.Fatal(err)
}
defer client.Close()

sandbox, err := axern.NewSandbox(axern.SandboxOptions{
    Client: client,
    TemplateID: "python311",
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

Prefer `defer sandbox.Close(ctx)` for SDK-owned sandboxes. Branch on helpers
such as `IsNotFound`, `IsTimeout`, and `IsValidation` rather than parsing error
text.

- [Go SDK source and full guide](https://github.com/cofy-x/axern/tree/main/sdk/go)
- [Runnable Go examples](https://github.com/cofy-x/axern/tree/main/sdk/go/examples)
