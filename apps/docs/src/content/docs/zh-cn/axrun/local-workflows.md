---
title: TaskSet 与本地工作流
description: 编译不可变 TaskSet、校验运行目录，并导出用于训练和评估的轨迹。
---

[托管 Rollout](/zh-cn/axrun/) 运行在持久控制面之上。围绕它的本地工作流——编译 TaskSet、检查运行目录、导出衍生视图——在本地开发和生产证据场景下行为一致。

## 编译 TaskSet

TaskSetBuild Spec 是严格的 `axrun/v1` YAML。编译器把指令和 Workspace 展开确定性地解析为不可变的 `TaskInstance` 记录；它从不创建隐式的笛卡尔积：

```bash
axrun task init --output-dir tasks/demo
axrun task build --file tasks/demo/taskset.yaml --output .axrun/tasksets/demo
axrun task inspect .axrun/tasksets/demo
```

`task init` 为起始任务写入显式的 `250m` CPU 和 `512Mi` 内存 request。请按实际 Agent 工作负载调整这些单 Episode 的 request，而不是依赖控制面回退值。在 Axern 暴露可强制执行的临时磁盘契约之前，`resources.disk` 会被拒绝。

本地 Bundle 支持编译器开发。托管 Rollout 要求通过 Kova 发布的不可变 `repository@sha256:...` 引用：

```bash
export KOVA_ENDPOINT=https://kova.example.com
export KOVA_TOKEN=...
axrun task publish .axrun/tasksets/demo \
  --target registry.example.com/axrun/tasksets/demo \
  --publisher kova
```

## 运行目录

每个 Rollout 都会写出一个可移植的运行目录，它是校验和导出的真相源：

```text
.axrun/runs/<run_id>/
  run.json
  plan.json
  inputs/
  tasks/<task_id>/task.json
  episodes/<episode_id>/episode.json
  episodes/<episode_id>/trajectory.jsonl
  episodes/<episode_id>/agent.json
  episodes/<episode_id>/verifier.json
  episodes/<episode_id>/reward.json
  episodes/<episode_id>/artifacts/manifest.json
  exports/
```

`run.json` 和 `plan.json` 冻结 Rollout 意图；Episode 的 Sidecar 文件记录执行、Agent 行为、验证、奖励、轨迹和 Artifact 引用。所有引用都相对运行目录根，因此整个目录可以整体移动。

## 校验和导出

Schema 校验是导出前的闸门：

```bash
axrun validate .axrun/runs/<run_id>

axrun export sft .axrun/runs/<run_id> --output-file sft.jsonl
axrun export reward .axrun/runs/<run_id>
axrun export trace .axrun/runs/<run_id>
axrun export preference .axrun/runs/<run_id>
```

导出是衍生视图——SFT 的 Prompt 和输出、Reward 行、轨迹 Trace、chosen/rejected 偏好对——都可以从运行目录复现。原始 LLM 遥测和命令日志保持为被引用的 Artifact，从不内联为字段。

终态退出码稳定，可用于自动化：`0` 通过，`10` 任务或验证器失败，`11` 基础设施失败，`12` 预算或计量失败，`13` 已取消，`14` 规划拒绝，`1` 客户端错误，`2` 用法错误。

[使用约定](https://github.com/cofy-x/axern/blob/main/apps/axrun/docs/usage.md)和 [领域模型](https://github.com/cofy-x/axern/blob/main/apps/axrun/docs/domain-model.md) 是权威参考。
