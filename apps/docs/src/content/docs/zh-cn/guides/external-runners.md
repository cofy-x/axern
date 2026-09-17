---
title: 外部 runner
description: 只使用 Axern 正式发布的公共 SDK 构建 inference 与 verification 工作流。
---

Axern 负责环境执行。外部 runner 负责 Episode、agent policy、CandidateBundle 与 VerificationResult schema、trajectory、评分和持久发布。它只保存公共的 `environment_id`、`run_id` 和 `allocation_id`；Node、runtime、lease、access grant 和路由身份均保持为内部实现。

可恢复路径为：

```text
解析后的输入
  -> Environment
  -> inference Run / Allocation
  -> 封存的声明 candidate
  -> 全新的 verification Run / Allocation
  -> 封存的声明 verification result
  -> 调用方自持的持久存储
```

在不可变 Run spec 中声明有限输出。节点先静止 Allocation，再在删除 runtime 前原子封存普通文件或目录 tar。调用方通过 `run_id` 查询 manifest，校验完整下载的 size 与 SHA-256，并在 15 分钟过期前把接受的字节复制到自持存储。节点磁盘丢失仍会返回明确的不可用结果。

Inference 与 verification 不共享可写文件系统。使用公共 File 或 Archive API 把下载后的 candidate 上传到新的 verifier Allocation。SSH、Terminal 和 Tunnel 只用于诊断或交互，不是持久结果通道。

限制为 16 个路径、单文件 64 MiB、单 tar 256 MiB、总计 256 MiB。缺失、拒绝、捕获失败和节点不可用都是 manifest 状态，而不是部分成功。Axern 不解释 candidate 或 verifier schema，也不提供 Artifact service、Dataset registry、持久 Workspace 或对象存储。

恢复与进程流语义详见仓库中的[外部 runner 集成契约](https://github.com/cofy-x/axern/blob/main/docs/product/external-runner-integration.md)。
