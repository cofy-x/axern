---
title: 节点网络
description: Axern 如何把 Sandbox 接入网络——eBPF NAT 数据面、报文路径和显式回退。
---

Axern 的节点网络使用嵌入 `axnoded` 的仓库内 IPv4 eBPF 出站 NAT 库 `bpfnet`。它是受支持 Linux 节点上的默认生产 NAT 数据面；IPv6 必须显式选择独立的 iptables 后端。

## 报文路径

| 路径 | 程序 |
| --- | --- |
| Sandbox TCP/UDP/ICMP 出站 SNAT | TC egress |
| Sandbox 出站回复恢复 | TC ingress |
| Native-routing CIDR 跳过 | TC egress |

## 职责划分

`axnoded` 掌管 Allocation 生命周期、bridge/veth/netns 资源、后端选择、回滚策略和 SNAT GC 调度。`bpfnet` 掌管出站数据面挂接与 reconcile、固定 map/程序和状态采集。入站访问由 Allocation-scoped Tunnel 或 SSH 会话提供；节点全局端点发布不属于该模型。

## 回退语义

回退是显式状态，不是静默降级。eBPF 后端要求两个 TC 方向和当前全部固定对象均可用。Helm Chart 默认 `node.network.natBackend` 为 `ebpf`；只在显式回滚或 IPv6 场景设为 `iptables`。

工程约定——挂接生命周期、生产替换门禁、回归 Runbook 和告警——随模块存放在仓库的 [`network/bpfnet/docs/`](https://github.com/cofy-x/axern/tree/main/network/bpfnet/docs) 和 [Helm Chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern)。
