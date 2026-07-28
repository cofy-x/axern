use super::{PeerRuntime, ShutdownHandle, DISCOVERY_REFRESH_SECS, DISCOVERY_TTL_SECS};
use anyhow::{bail, Context};
use redis::aio::MultiplexedConnection;
use redis::AsyncCommands;
use std::net::SocketAddr;
use std::sync::{Arc, RwLock};
use tokio::task::JoinHandle;
use tokio::time::interval;
use tracing::debug;

pub trait PeerDiscovery: Send + Sync {
    fn get_peers(&self) -> Vec<SocketAddr>;
    fn shutdown(&self) {}
}

#[derive(Debug)]
pub struct StaticPeers {
    peers: Vec<SocketAddr>,
}

impl StaticPeers {
    pub fn new(peers: Vec<SocketAddr>) -> Self {
        Self { peers }
    }
}

impl PeerDiscovery for StaticPeers {
    fn get_peers(&self) -> Vec<SocketAddr> {
        self.peers.clone()
    }
}

pub struct RedisDiscovery {
    peers: Arc<RwLock<Vec<SocketAddr>>>,
    shutdown: ShutdownHandle,
    _worker: JoinHandle<()>,
}

impl std::fmt::Debug for RedisDiscovery {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("RedisDiscovery").finish()
    }
}

impl RedisDiscovery {
    pub fn new(
        runtime: PeerRuntime,
        url: &str,
        advertise_addr: SocketAddr,
        node_id: &str,
    ) -> anyhow::Result<Self> {
        let peers = Arc::new(RwLock::new(Vec::new()));
        let shutdown = ShutdownHandle::new();
        let discovery_worker = RedisDiscoveryWorker::new(
            url,
            advertise_addr,
            node_id,
            Arc::clone(&peers),
            shutdown.clone(),
        )?;
        let worker = runtime.spawn(discovery_worker.run());
        Ok(Self {
            peers,
            shutdown,
            _worker: worker,
        })
    }
}

impl PeerDiscovery for RedisDiscovery {
    fn get_peers(&self) -> Vec<SocketAddr> {
        self.peers.read().unwrap().clone()
    }

    fn shutdown(&self) {
        self.shutdown.shutdown();
    }
}

struct RedisDiscoveryWorker {
    client: redis::Client,
    conn: Option<MultiplexedConnection>,
    key: String,
    value: String,
    advertise_addr: SocketAddr,
    peers: Arc<RwLock<Vec<SocketAddr>>>,
    shutdown: ShutdownHandle,
}

impl RedisDiscoveryWorker {
    fn new(
        url: &str,
        advertise_addr: SocketAddr,
        node_id: &str,
        peers: Arc<RwLock<Vec<SocketAddr>>>,
        shutdown: ShutdownHandle,
    ) -> anyhow::Result<Self> {
        let client = redis::Client::open(url).context("failed to create redis discovery client")?;
        Ok(Self {
            client,
            conn: None,
            key: format!("imagefsd:peers:{node_id}"),
            value: advertise_addr.to_string(),
            advertise_addr,
            peers,
            shutdown,
        })
    }

    async fn run(mut self) {
        let mut ticker = interval(std::time::Duration::from_secs(DISCOVERY_REFRESH_SECS));
        loop {
            match self.refresh_peers().await {
                Ok(new_peers) => *self.peers.write().unwrap() = new_peers,
                Err(err) => {
                    self.conn = None;
                    debug!(err = debug(err), "failed to refresh redis peers");
                }
            }

            tokio::select! {
                _ = self.shutdown.wait() => {
                    if let Err(err) = self.delete_peer().await {
                        debug!(err = debug(err), "failed to delete redis peer");
                    }
                    return;
                }
                _ = ticker.tick() => {}
            }
        }
    }

    async fn refresh_peers(&mut self) -> anyhow::Result<Vec<SocketAddr>> {
        let mut conn = self.ensure_conn().await?;
        let _: () = conn
            .set_ex(&self.key, &self.value, DISCOVERY_TTL_SECS)
            .await?;
        let keys: Vec<String> = conn.keys("imagefsd:peers:*").await?;
        let vals: Vec<Option<String>> = if keys.is_empty() {
            Vec::new()
        } else {
            redis::cmd("MGET").arg(&keys).query_async(&mut conn).await?
        };
        Ok(vals
            .into_iter()
            .flatten()
            .filter_map(|addr| addr.parse::<SocketAddr>().ok())
            .filter(|addr| *addr != self.advertise_addr)
            .collect())
    }

    async fn delete_peer(&mut self) -> anyhow::Result<()> {
        let mut conn = self.ensure_conn().await?;
        let _: usize = conn.del(&self.key).await?;
        Ok(())
    }

    async fn ensure_conn(&mut self) -> anyhow::Result<MultiplexedConnection> {
        if let Some(conn) = self.conn.as_ref() {
            return Ok(conn.clone());
        }
        let conn = self
            .client
            .get_multiplexed_async_connection()
            .await
            .context("failed to connect to redis discovery")?;
        self.conn = Some(conn.clone());
        Ok(conn)
    }
}

pub fn parse_peer_addrs(peer_addrs: &str) -> anyhow::Result<Vec<SocketAddr>> {
    let mut peers = Vec::new();
    for addr in peer_addrs
        .split(',')
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        peers.push(
            addr.parse()
                .with_context(|| format!("invalid peer addr: {addr}"))?,
        );
    }
    Ok(peers)
}

pub fn build_discovery(
    runtime: PeerRuntime,
    peer_discovery: &str,
    peer_addrs: &str,
    advertise_addr: SocketAddr,
    node_id: &str,
) -> anyhow::Result<Arc<dyn PeerDiscovery>> {
    if peer_discovery.starts_with("redis://") {
        return Ok(Arc::new(RedisDiscovery::new(
            runtime,
            peer_discovery,
            advertise_addr,
            node_id,
        )?));
    }
    if !peer_discovery.trim().is_empty() {
        let _ = (runtime, advertise_addr, node_id);
        bail!(
            "unsupported peer discovery configuration: {}",
            peer_discovery
        );
    }
    Ok(Arc::new(StaticPeers::new(parse_peer_addrs(peer_addrs)?)))
}
