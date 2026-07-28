# Local Runtime Troubleshooting

Use this runbook for the repo-supported compose and kind development
environments. It focuses on commands: how to get status, how to collect the
critical logs, and which command to run for each failure mode.

For the environment-neutral meaning of each component log, see
[Runtime Logs](../../docs/operations/runtime-logs.md).

## Quick Commands

Compose:

```bash
make local-compose-status
docker ps --filter 'name=axern-local'
docker logs --tail=160 axern-local-controld-1
docker logs --tail=160 axern-local-node-1
docker logs --tail=160 axern-local-gatewayd-1
```

Kind:

```bash
make kind-status
eval "$(make kube-env-kind)"
kubectl -n axern-local get pods -o wide
kubectl -n axern-local logs deploy/controld --tail=160
kubectl -n axern-local logs -l app=node-all-in-one --tail=160
kubectl -n axern-local logs deploy/gatewayd --tail=160
```

Node-local checks:

```bash
# compose
docker exec axern-local-node-1 axctl node check
docker exec axern-local-node-1 axctl sandbox list
docker exec axern-local-node-1 axctl image list
docker exec axern-local-node-1 axctl image mounts

# kind
NODE_POD="$(kubectl -n axern-local get pods -l app=node-all-in-one -o jsonpath='{.items[0].metadata.name}')"
kubectl -n axern-local exec "${NODE_POD}" -- axctl node check
kubectl -n axern-local exec "${NODE_POD}" -- axctl sandbox list
kubectl -n axern-local exec "${NODE_POD}" -- axctl image list
kubectl -n axern-local exec "${NODE_POD}" -- axctl image mounts
```

Health surfaces:

```bash
# compose
curl -fsS http://127.0.0.1:24101/healthz
curl -fsS http://127.0.0.1:24101/nodesz
curl -fsS http://127.0.0.1:25080/healthz

# kind
curl -fsS http://127.0.0.1:24211/healthz
curl -fsS http://127.0.0.1:24211/nodesz
curl -fsS http://127.0.0.1:25082/healthz
```

## Critical Logs

### Compose

Service logs:

| Component | Command |
| --- | --- |
| `controld` | `docker logs --tail=200 axern-local-controld-1` |
| `controld-migrate` | `docker logs --tail=200 axern-local-controld-migrate-1` |
| `tunneld` | `docker logs --tail=200 axern-local-tunneld-1` |
| `node-all-in-one` | `docker logs --tail=200 axern-local-node-1` |
| `gatewayd` | `docker logs --tail=200 axern-local-gatewayd-1` |
| `postgres` | `docker logs --tail=120 axern-local-postgres-1` |
| `minio` | `docker logs --tail=120 axern-local-minio-1` |

Node-internal logs:

```bash
docker exec axern-local-node-1 tail -n 200 /var/log/axnoded/axnoded.log
docker exec axern-local-node-1 tail -n 200 /var/log/axnoded/node-tunneld.log
docker exec axern-local-node-1 tail -n 200 /var/lib/imagemgr/logs/imagemgr.log
docker exec axern-local-node-1 sh -lc 'find /var/lib/imagemgr/daemons -maxdepth 3 -name daemon.log -print -exec tail -n 80 {} \;'
```

Config:

```bash
docker exec axern-local-node-1 sed -n '1,220p' /tmp/axnoded-node-config.toml
```

### Kind

Service logs:

| Component | Command |
| --- | --- |
| `controld` | `kubectl -n axern-local logs deploy/controld --tail=200` |
| `controld-migrate` | `kubectl -n axern-local logs job/controld-migrate --tail=200` |
| `tunneld` | `kubectl -n axern-local logs deploy/tunneld --tail=200` |
| `node-all-in-one` | `kubectl -n axern-local logs -l app=node-all-in-one --tail=200` |
| `gatewayd` | `kubectl -n axern-local logs deploy/gatewayd --tail=200` |
| `postgres` | `kubectl -n axern-local logs deploy/postgres --tail=120` |
| `minio` | `kubectl -n axern-local logs deploy/minio --tail=120` |

Node-internal logs:

```bash
NODE_POD="$(kubectl -n axern-local get pods -l app=node-all-in-one -o jsonpath='{.items[0].metadata.name}')"
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 200 /var/log/axnoded/axnoded.log
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 200 /var/log/axnoded/node-tunneld.log
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 200 /var/lib/imagemgr/logs/imagemgr.log
kubectl -n axern-local exec "${NODE_POD}" -- sh -lc 'find /var/lib/imagemgr/daemons -maxdepth 3 -name daemon.log -print -exec tail -n 80 {} \;'
```

Config:

```bash
kubectl -n axern-local exec "${NODE_POD}" -- sed -n '1,220p' /tmp/axnoded-node-config.toml
```

## Troubleshooting By Symptom

### Environment Is Not Healthy

Compose:

```bash
make local-compose-status
docker ps --filter 'name=axern-local'
docker logs --tail=120 axern-local-controld-migrate-1
docker logs --tail=120 axern-local-controld-1
docker logs --tail=120 axern-local-node-1
```

Kind:

```bash
make kind-status
kubectl -n axern-local get pods -o wide
kubectl -n axern-local describe pod -l app=node-all-in-one
kubectl -n axern-local logs job/controld-migrate --tail=120
kubectl -n axern-local logs deploy/controld --tail=120
kubectl -n axern-local logs -l app=node-all-in-one --tail=120
```

### Workload Is Not Scheduled

Compose:

```bash
curl -fsS http://127.0.0.1:24101/nodesz
docker logs --tail=200 axern-local-controld-1
docker exec axern-local-node-1 tail -n 200 /var/log/axnoded/axnoded.log
```

Kind:

```bash
curl -fsS http://127.0.0.1:24211/nodesz
kubectl -n axern-local logs deploy/controld --tail=200
NODE_POD="$(kubectl -n axern-local get pods -l app=node-all-in-one -o jsonpath='{.items[0].metadata.name}')"
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 200 /var/log/axnoded/axnoded.log
```

### Sandbox Creation Fails

Compose:

```bash
docker logs --tail=160 axern-local-controld-1
docker exec axern-local-node-1 axctl sandbox list
docker exec axern-local-node-1 tail -n 240 /var/log/axnoded/axnoded.log
```

Kind:

```bash
kubectl -n axern-local logs deploy/controld --tail=160
NODE_POD="$(kubectl -n axern-local get pods -l app=node-all-in-one -o jsonpath='{.items[0].metadata.name}')"
kubectl -n axern-local exec "${NODE_POD}" -- axctl sandbox list
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 240 /var/log/axnoded/axnoded.log
```

### Image Or Rootfs Fails

Compose:

```bash
docker exec axern-local-node-1 axctl image list
docker exec axern-local-node-1 axctl image mounts
docker exec axern-local-node-1 tail -n 200 /var/log/axnoded/axnoded.log
docker exec axern-local-node-1 tail -n 240 /var/lib/imagemgr/logs/imagemgr.log
docker exec axern-local-node-1 sh -lc 'find /var/lib/imagemgr/daemons -maxdepth 3 -name daemon.log -print -exec tail -n 80 {} \;'
```

Kind:

```bash
NODE_POD="$(kubectl -n axern-local get pods -l app=node-all-in-one -o jsonpath='{.items[0].metadata.name}')"
kubectl -n axern-local exec "${NODE_POD}" -- axctl image list
kubectl -n axern-local exec "${NODE_POD}" -- axctl image mounts
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 200 /var/log/axnoded/axnoded.log
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 240 /var/lib/imagemgr/logs/imagemgr.log
kubectl -n axern-local exec "${NODE_POD}" -- sh -lc 'find /var/lib/imagemgr/daemons -maxdepth 3 -name daemon.log -print -exec tail -n 80 {} \;'
```

### Gateway HTTP Or Terminal Fails

Compose:

```bash
curl -fsS http://127.0.0.1:25080/healthz
docker logs --tail=200 axern-local-gatewayd-1
docker logs --tail=200 axern-local-controld-1
docker exec axern-local-node-1 tail -n 160 /var/log/axnoded/axnoded.log
```

Kind:

```bash
curl -fsS http://127.0.0.1:25082/healthz
kubectl -n axern-local logs deploy/gatewayd --tail=200
kubectl -n axern-local logs deploy/controld --tail=200
NODE_POD="$(kubectl -n axern-local get pods -l app=node-all-in-one -o jsonpath='{.items[0].metadata.name}')"
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 160 /var/log/axnoded/axnoded.log
```

### Tunnel Fails

Compose:

```bash
docker logs --tail=200 axern-local-tunneld-1
docker logs --tail=200 axern-local-controld-1
docker exec axern-local-node-1 tail -n 240 /var/log/axnoded/node-tunneld.log
docker exec axern-local-node-1 tail -n 160 /var/log/axnoded/axnoded.log
```

Kind:

```bash
kubectl -n axern-local logs deploy/tunneld --tail=200
kubectl -n axern-local logs deploy/controld --tail=200
NODE_POD="$(kubectl -n axern-local get pods -l app=node-all-in-one -o jsonpath='{.items[0].metadata.name}')"
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 240 /var/log/axnoded/node-tunneld.log
kubectl -n axern-local exec "${NODE_POD}" -- tail -n 160 /var/log/axnoded/axnoded.log
```

## Restart And Refresh

Prefer the environment refresh path after code or image changes:

```bash
make local-compose-refresh
make kind-refresh
```

Use targeted smokes after a suspected fix:

```bash
make local-compose-smoke
make local-compose-gateway-smoke
make local-compose-tunnel-e2e
make local-compose-image-service-smoke

make kind-smoke
make kind-gateway-smoke
make kind-tunnel-e2e
make kind-image-service-smoke
```

Use reset only when local state is suspect:

```bash
make local-compose-reset
make kind-reset
```
