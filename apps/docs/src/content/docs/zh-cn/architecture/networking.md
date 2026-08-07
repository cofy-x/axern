---
title: 节点网络
description: Axern 如何把 Sandbox 接入网络——eBPF NAT 数据面、报文路径和显式回退。
---

Axern 的节点网络由 `bpfnet` 处理，它是仓库内的 Linux eBPF NAT 数据面库。它嵌入 `axnoded` 运行，而不是独立的 daemon，是受支持 Linux 节点上的默认生产 NAT 数据面。其范围为 IPv4；Linux localhost UDP 和 IPv6 不在支持的设计之内。

## 报文路径

| 路径 | 程序 |
| --- | --- |
| 外部 TCP/UDP hostPort DNAT 到 Sandbox 目标 | TC ingress |
| Service 回复的源地址恢复到节点 hostPort | TC egress |
| Sandbox TCP/UDP/ICMP 出站 SNAT | TC egress |
| Sandbox 出站回复恢复 | TC ingress |
| 主机本地 TCP hostPort 兼容 | cgroup `connect4`、`getpeername4`、`sock_release` |
| Native-routing CIDR 跳过 | TC egress |

## 职责划分

`axnoded` 掌管 Sandbox 生命周期、bridge/veth/netns 资源、Service hostPort 意图、后端选择、回滚策略和 SNAT GC 调度。`bpfnet` 掌管数据面挂接与 reconcile、service map 编程、固定的 map 和程序，以及状态采集。`bpfnetctl` 是只读诊断工具，从不写入 Service 意图。

## 回退语义

回退是显式状态，不是静默降级。当 localhost TCP cgroup 路径不可用时，axnoded 可以对 localhost TCP 兼容使用 iptables，同时 TC ingress 和 egress 保持在 eBPF 上。完全回退到 iptables 是回滚状态，不是 eBPF 替换成功。Helm Chart 默认 `node.network.natBackend` 为 `ebpf`；只在显式回滚时设为 `iptables`。

工程约定——挂接生命周期、生产替换门禁、回归 Runbook 和告警——随模块存放在仓库的 [`network/bpfnet/docs/`](https://github.com/cofy-x/axern/tree/main/network/bpfnet/docs) 和 [Helm Chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern)。
