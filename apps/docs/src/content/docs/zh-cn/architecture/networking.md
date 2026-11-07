---
title: 节点网络
description: Sandbox 网络的显式 NAT 后端选择和职责划分。
---

`axnoded` 掌管 Sandbox 网络命名空间、接口和 NAT 后端选择。[原生 Linux 安装](https://github.com/cofy-x/axern/blob/main/deploy/standalone/INSTALL.md)配置 iptables 后端。Helm Chart 默认选择 `ebpf`，使用嵌入 `axnoded` 的仓库内 IPv4 `bpfnet` 库。IPv6 必须显式选择 iptables。

## eBPF 报文路径

| 路径 | 程序 |
| --- | --- |
| Sandbox TCP/UDP/ICMP 出站 SNAT | TC egress |
| Sandbox 出站回复恢复 | TC ingress |
| Native-routing CIDR 跳过 | TC egress |

## 职责划分

`axnoded` 掌管 Allocation 生命周期、bridge/veth/netns 资源、后端选择、回滚策略和 SNAT GC 调度。`bpfnet` 掌管出站数据面挂接与 reconcile、固定 map/程序和状态采集。入站访问由 Allocation-scoped Tunnel 或 SSH 会话提供；节点全局端点发布不属于该模型。

## 后端配置

eBPF 后端要求两个 TC 方向和当前全部固定对象均可用。通过 Helm values 中的 `node.network.natBackend`，为该安装的网络配置选择 `ebpf` 或 `iptables`；IPv6 使用 iptables。后端健康和配置的回退都是显式运行状态。

工程约定——挂接生命周期、生产替换门禁、回归 Runbook 和告警——随模块存放在仓库的 [`network/bpfnet/docs/`](https://github.com/cofy-x/axern/tree/main/network/bpfnet/docs) 和 [Helm Chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern)。
