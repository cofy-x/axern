---
title: Kubernetes 安装
description: 使用官方 Helm Chart 在 Kubernetes 上安装 Axern 并连接 CLI。
---

Axern 的云中立 Chart 以 OCI Artifact 形式发布在 GHCR。Chart 支持独立的平台、可观测和运行时调度 Profile，生产集群可以把控制面服务与 Sandbox 容量隔离在不同节点池。

本页描述基于本地 port-forward 的评估路径。需要 `kubectl`、Helm 3，以及与 Chart 同一 Release 的 `axern` CLI 归档。继续之前请先从 [Axern Releases](https://github.com/cofy-x/axern/releases) 页面下载归档及其校验和。下文 `<version>` 为 Release 版本号，不带 `v` 前缀。

## 安装 Chart

先创建命名空间、Axern 自有签发材料，并为每个 Kubernetes Node 生成独立的一次性注册 token。Secret key 是 Axern Node ID（独立于 Kubernetes Node 名称的新随机身份）；在下文完成对应身份准入前保留这些临时文件。

```bash
kubectl create namespace axern-system

axern admin pki bootstrap --directory ./axern-pki --cluster axern.local \
  --dns localhost,controld,gatewayd,tunneld,controld.axern-system.svc,gatewayd.axern-system.svc,tunneld.axern-system.svc
kubectl -n axern-system create secret generic axern-pki \
  --from-file=ca.crt=./axern-pki/ca.crt \
  --from-file=controld.pem=./axern-pki/controld.pem \
  --from-file=gatewayd.pem=./axern-pki/gatewayd.pem \
  --from-file=tunneld.pem=./axern-pki/tunneld.pem \
  --from-file=client.crt=./axern-pki/client.crt \
  --from-file=client.key=./axern-pki/client.key
kubectl -n axern-system create secret generic axern-pki-signer \
  --from-file=signer.pem=./axern-pki/private/signer.pem

enrollment_token_dir="$(mktemp -d)"
node_secret_args=()
node_helm_args=()
node_index=0
for kubernetes_node in $(kubectl get nodes -o name | sed 's#node/##'); do
  axern_node_id="node-$(openssl rand -hex 16)"
  node_helm_args+=(--set-string "node.enrollment.nodes[${node_index}].nodeName=${kubernetes_node}"
                    --set-string "node.enrollment.nodes[${node_index}].nodeID=${axern_node_id}")
  node_index=$((node_index + 1))
  openssl rand -hex 32 > "${enrollment_token_dir}/${axern_node_id}"
  chmod 600 "${enrollment_token_dir}/${axern_node_id}"
  node_secret_args+=(--from-file="${axern_node_id}=${enrollment_token_dir}/${axern_node_id}")
done
kubectl --namespace axern-system create secret generic axern-enrollment-tokens "${node_secret_args[@]}"

: "${AXERN_NODE_MEMORY_SYSTEM_RESERVE_BYTES:?set the qualified node memory reserve in bytes}"
helm install axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  --set-string node.enrollment.existingSecret=axern-enrollment-tokens \
  --set-string "node.memorySystemReserveBytes=${AXERN_NODE_MEMORY_SYSTEM_RESERVE_BYTES}" \
  "${node_helm_args[@]}"

kubectl -n axern-system rollout status deployment/controld --timeout=5m
kubectl -n axern-system rollout status deployment/gatewayd --timeout=5m
```

首次安装有意不在 Node admission 前等待节点就绪。内存预留值必须来自节点资格验证结果，不得为通过 Helm 校验而随意填入小值。控制面启动并完成准入后，下文的 DaemonSet rollout 检查负责确认节点就绪。默认镜像是同一 Release 的不可变版本 Tag。环境相关的配置放在自己的 values 文件中，用 `-f values.yaml` 传入。

## 不启用 SSH 连接 CLI

默认 Chart 暴露控制和 HTTP Gateway 端口。在另一个终端中保持 port-forward：

```bash
kubectl --namespace axern-system port-forward svc/gatewayd \
  25100:25000 25101:25080
```

回到保留 `enrollment_token_dir` 变量的原始 shell，把部署层初始化的管理员 mTLS 身份导入为本地 CLI Context。空的 SSH endpoint 是有意为之：Chart 默认禁用 SSH，而 Environment、Run 和 SDK 工作流都不需要它。

```bash
axern context import-kubernetes local \
  --namespace axern-system \
  --secret axern-pki \
  --endpoint 127.0.0.1:25100 \
  --ssh-endpoint "" \
  --current

for credential_path in "${enrollment_token_dir}"/node-*; do
  axern admin node admit "$(basename "${credential_path}")" \
    --enrollment-token-file "${credential_path}" \
    --operator-reason "initial Kubernetes node admission"
done

kubectl -n axern-system rollout status daemonset -l app.kubernetes.io/component=node --timeout=15m
axern doctor --namespace default
axern environment list
```

导入的 Context 携带控制面 endpoint 和 TLS 材料。节点报告在准入成功前保持 fail-closed。除非显式启用 SSH 并提供客户端身份，SSH 字段保持为空。之后所有控制面工作流与本地 Compose 安装使用同一套 Context 模型。

## 为交互式 Agent 工作流启用 SSH

SSH 是 Gateway 的可选功能，供交互式 Agent 和 SSH 指南使用。用单独的 values 文件启用监听器；客户端公钥通过 AccessAdmin 注册，不进入 Helm values：

```yaml title="ssh-values.yaml"
gatewayd:
  ssh:
    enabled: true
```

把 values 文件应用到同一 Release，然后转发 SSH 端口：

```bash
helm upgrade axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  -f ssh-values.yaml \
  --reuse-values \
  --wait \
  --timeout 15m

kubectl --namespace axern-system port-forward svc/gatewayd 25122:25022
```

先把公钥注册为具有目标命名空间执行权限的 Principal Credential，再导入或更新 Context：

```bash
axern admin credential add <principal-id> --ssh-public-key ~/.ssh/id_ed25519.pub \
  --expires-at 2027-01-01T00:00:00Z --label workstation
axern context import-kubernetes local \
  --namespace axern-system \
  --secret axern-pki \
  --endpoint 127.0.0.1:25100 \
  --ssh-endpoint 127.0.0.1:25122 \
  --ssh-identity-file ~/.ssh/id_ed25519 \
  --current
```

选择适合部署的凭据有效期，并为对应 Principal 授予必要的命名空间角色。限制 SSH 私钥权限，在共享集群使用前验证持久的 Gateway 主机密钥。

## 节点身份运维

将显式的 `nodeName` / `nodeID` 绑定保存到部署 values，普通升级不得重新生成身份。注册 token 是独立只读文件，不进入节点配置或环境变量。证书持久发布后可移除 token Secret；节点重启和自动续期不再读取它。续期在证书剩余 7–8 小时时触发，失败采用有界、带抖动的退避重试。

使用 `axern admin node revoke <node-id> --operator-reason <reason>` 可立即撤回授权，不要求节点空闲。这不代表资源已经清理：终止和释放仍由 Allocation 生命周期负责，已签发或进行中的授权受原有 TTL 限制。被攻陷的主机还必须在外部隔离。`node retire` 仍须等清理与访问义务收敛后才能执行。

主机替换、状态丢失或身份过期必须使用新 Node ID 和新节点状态，即使 Kubernetes 复用了同一主机名。先隔离并清理旧主机；不得通过删除运行中状态或复用 token 绕过恢复约束。

## 持久化部署之前

内置 PostgreSQL 和单节点默认值仅用于评估。运行共享或生产工作负载前，请审视以下 Chart 配置项：

- **Release 产物：** 统一锁定 Chart、镜像和 CLI 版本，安装 CLI 前校验其 checksum。
- **集群前提：** 确认所需的 Kubernetes/Helm 版本、`runsc` 运行时可用性、运行时与镜像服务所需的节点权限、默认 NAT 数据面所需的 eBPF 内核能力（`node.network.natBackend=iptables` 是显式回退项），以及每个调度节点到镜像仓库的可达性。
- **Gateway 暴露：** 用显式管理的 Service 或 Ingress 替换本地 port-forward，配置 TLS 服务器名称和网络策略；除非交互式工作流需要，保持 SSH 关闭。
- **Secret：** 用 `secrets.existingSecret` 提供 master key；用 `postgres.existingSecret` 提供数据库凭据；用 `node.enrollment.existingSecret` 为每个已准入 Node ID 提供独立一次性注册 token。在把 DaemonSet 调度到新的 Kubernetes Node 前，先加入对应的 `nodeID` key，并通过管理 API 准入该身份。
- **持久存储：** 设置 `postgres.persistence.enabled=true` 并搭配拓扑感知的 `ReadWriteOnce` StorageClass；不要在 `emptyDir` 回退上运行持久环境。
- **调度：** 为 `scheduling.platform`、`scheduling.observability` 和 `scheduling.runtime` 配置专用节点池标签和对应的 `NoSchedule` Taint。
- **可观测：** 内置的 Prometheus、Tempo、Loki、Grafana 栈是持久的但单副本；在 `observability` 下规划保留周期和存储容量。

:::caution[1.0 前的安全边界]

Axern 不声称默认安装可安全承载不可信的多租户工作负载。TLS、Ingress、镜像信任、网络策略、Secret 存储、配额和持久存储均由运维者负责。

:::

[Helm Chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern) 是 values、节点网络和有状态依赖的权威参考。
