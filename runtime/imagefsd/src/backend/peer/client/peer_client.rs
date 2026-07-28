use super::health::{PeerHealthMetrics, PeerHealthTracker};
use super::hints::PeerHitHints;
use crate::backend::chunkdb::CheckSum;
use crate::backend::peer::discovery::PeerDiscovery;
use crate::backend::peer::index::ChunkIndex;
use crate::backend::peer::metrics::PeerClientMetrics;
use crate::backend::peer::protocol::{MessageType, Request};
use crate::backend::peer::session::{MultiplexedSession, PeerPoolMap, TcpConnPool};
use crate::backend::peer::{
    timeout_error, PeerRuntime, DEFAULT_MAX_QUERY_PEERS, DEFAULT_TIMEOUT_MS, HINT_GC_INTERVAL_SECS,
};
use opentelemetry::global;
use rand::seq::SliceRandom;
use std::collections::HashMap;
use std::io::{self, ErrorKind};
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::net::TcpStream;
use tokio::sync::Mutex;
use tokio::task::JoinHandle;
use tokio::time::{interval, timeout};
use tracing::debug;

pub struct PeerClient {
    runtime: PeerRuntime,
    pub(in crate::backend::peer) discovery: Arc<dyn PeerDiscovery>,
    pub(in crate::backend::peer) chunk_index: Option<Arc<dyn ChunkIndex>>,
    query_timeout: Duration,
    max_query_peers: usize,
    local_addr: Option<SocketAddr>,
    hit_hints: Arc<PeerHitHints>,
    pub(in crate::backend::peer) health: Arc<PeerHealthTracker>,
    pub(in crate::backend::peer) pools: PeerPoolMap,
    pub(in crate::backend::peer) metrics: PeerClientMetrics,
    _health_metrics: PeerHealthMetrics,
}

impl std::fmt::Debug for PeerClient {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PeerClient")
            .field("query_timeout", &self.query_timeout)
            .field("max_query_peers", &self.max_query_peers)
            .finish()
    }
}

impl PeerClient {
    pub fn new(runtime: PeerRuntime, discovery: Arc<dyn PeerDiscovery>) -> Self {
        let meter = global::meter("imagefsd.peer");
        let health = Arc::new(PeerHealthTracker::default());
        let health_metrics = health.register_metrics(&meter);
        Self {
            runtime,
            discovery,
            chunk_index: None,
            query_timeout: Duration::from_millis(DEFAULT_TIMEOUT_MS),
            max_query_peers: DEFAULT_MAX_QUERY_PEERS,
            local_addr: None,
            hit_hints: Arc::new(PeerHitHints::default()),
            health,
            pools: Arc::new(Mutex::new(HashMap::new())),
            metrics: PeerClientMetrics::new(&meter),
            _health_metrics: health_metrics,
        }
    }

    pub fn with_chunk_index(mut self, chunk_index: Arc<dyn ChunkIndex>) -> Self {
        self.chunk_index = Some(chunk_index);
        self
    }

    pub fn with_local_addr(mut self, local_addr: SocketAddr) -> Self {
        self.local_addr = Some(local_addr);
        self
    }

    pub(in crate::backend::peer) fn locality_counts(&self) -> (u64, u64, u64) {
        let (healthy, unhealthy) = self.health.counts();
        let hinted = self.hit_hints.hinted_peer_count();
        (healthy, unhealthy, hinted)
    }

    #[cfg(test)]
    #[allow(dead_code)]
    pub fn with_timeout(mut self, query_timeout: Duration) -> Self {
        self.query_timeout = query_timeout;
        self
    }

    pub(in crate::backend::peer) fn start_background_tasks(
        &self,
        shutdown: super::super::ShutdownHandle,
    ) -> JoinHandle<()> {
        let hints = Arc::clone(&self.hit_hints);
        self.runtime.spawn(async move {
            let mut ticker = interval(Duration::from_secs(HINT_GC_INTERVAL_SECS));
            loop {
                tokio::select! {
                    _ = shutdown.wait() => break,
                    _ = ticker.tick() => hints.gc_expired(),
                }
            }
        })
    }

    fn is_self(&self, addr: &SocketAddr) -> bool {
        self.local_addr.is_some_and(|local| local == *addr)
    }

    pub(in crate::backend::peer) fn ranked_peers_with_scores(
        &self,
        mut peers: Vec<SocketAddr>,
        scores: &HashMap<SocketAddr, f64>,
    ) -> Vec<SocketAddr> {
        let health = self
            .health
            .snapshot()
            .entries
            .into_iter()
            .map(|entry| (entry.addr, (entry.healthy, entry.avg_rtt_ms)))
            .collect::<HashMap<SocketAddr, (bool, f64)>>();
        peers.retain(|addr| !self.is_self(addr));
        peers.sort_by(|a, b| {
            let (a_healthy, a_rtt_ms) = health.get(a).copied().unwrap_or((true, 50.0));
            let (b_healthy, b_rtt_ms) = health.get(b).copied().unwrap_or((true, 50.0));
            let a_unhealthy = !a_healthy;
            let b_unhealthy = !b_healthy;
            match a_unhealthy.cmp(&b_unhealthy) {
                std::cmp::Ordering::Equal => {
                    let a_score = scores.get(a).copied().unwrap_or_default() * 10.0
                        + 100.0 / a_rtt_ms.max(1.0);
                    let b_score = scores.get(b).copied().unwrap_or_default() * 10.0
                        + 100.0 / b_rtt_ms.max(1.0);
                    b_score
                        .partial_cmp(&a_score)
                        .unwrap_or(std::cmp::Ordering::Equal)
                }
                other => other,
            }
        });
        peers.dedup();
        peers
    }

    fn discovery_peers(&self) -> Vec<SocketAddr> {
        self.discovery
            .get_peers()
            .into_iter()
            .filter(|addr| !self.is_self(addr))
            .collect()
    }

    pub(in crate::backend::peer) async fn candidate_peers(
        &self,
        checksum: &CheckSum,
    ) -> (Vec<SocketAddr>, &'static str) {
        let scores = self.hit_hints.score_snapshot();
        if let Some(index) = &self.chunk_index {
            let owners = match index.lookup_owners(checksum).await {
                Ok(owners) => self.ranked_peers_with_scores(owners, &scores),
                Err(err) => {
                    debug!(err = debug(err), checksum = %checksum, "chunk index lookup failed");
                    Vec::new()
                }
            };
            if !owners.is_empty() {
                return (
                    owners.into_iter().take(self.max_query_peers).collect(),
                    "index",
                );
            }
        }

        let ranked = self.ranked_peers_with_scores(self.discovery_peers(), &scores);
        if ranked.is_empty() {
            return (ranked, "random");
        }

        let mut hinted = Vec::new();
        let mut unknown = Vec::new();
        for addr in ranked {
            if scores.get(&addr).copied().unwrap_or_default() > 0.0 {
                hinted.push(addr);
            } else {
                unknown.push(addr);
            }
        }

        if !hinted.is_empty() {
            let mut selected = hinted;
            if selected.len() < self.max_query_peers && !unknown.is_empty() {
                let mut rng = rand::thread_rng();
                unknown.shuffle(&mut rng);
                selected.extend(
                    unknown
                        .into_iter()
                        .take(self.max_query_peers - selected.len()),
                );
            }
            selected.truncate(self.max_query_peers);
            return (selected, "hithints");
        }

        let mut rng = rand::thread_rng();
        unknown.shuffle(&mut rng);
        unknown.truncate(self.max_query_peers);
        (unknown, "random")
    }

    #[cfg(test)]
    #[allow(dead_code)]
    pub fn fetch_chunk_blocking(&self, checksum: &CheckSum) -> Option<Vec<u8>> {
        self.runtime.block_on(self.fetch_chunk(checksum))
    }

    #[allow(dead_code)]
    pub fn health_check_blocking(&self, peer: SocketAddr) -> bool {
        self.runtime.block_on(self.health_check(peer))
    }

    pub async fn fetch_chunk(&self, checksum: &CheckSum) -> Option<Vec<u8>> {
        let begin_total = Instant::now();
        let (peers, source) = self.candidate_peers(checksum).await;
        let mut saw_miss = false;
        let mut saw_timeout = false;
        let mut saw_error = false;
        for (attempt, peer) in peers.into_iter().enumerate() {
            if source == "index" && attempt > 0 {
                self.metrics.record_retry(source);
            }
            let begin = Instant::now();
            match self.query_peer(peer, checksum).await {
                Ok(Some(data)) => {
                    let elapsed_ms = begin.elapsed().as_secs_f64() * 1000.0;
                    self.health.record_success(peer, elapsed_ms);
                    self.hit_hints.record_hit(peer, *checksum);
                    self.metrics.record_fetch(
                        source,
                        "hit",
                        begin_total.elapsed().as_secs_f64() * 1000.0,
                    );
                    return Some(data);
                }
                Ok(None) => {
                    saw_miss = true;
                    self.health.record_miss(peer);
                }
                Err(err) => {
                    debug!(peer = %peer, err = debug(&err), "peer query failed");
                    if err.kind() == ErrorKind::TimedOut {
                        saw_timeout = true;
                    } else {
                        saw_error = true;
                    }
                    self.health.record_failure(peer);
                }
            }
        }
        let result = if saw_miss {
            "miss"
        } else if saw_timeout {
            "timeout"
        } else if saw_error {
            "error"
        } else {
            "miss"
        };
        self.metrics
            .record_fetch(source, result, begin_total.elapsed().as_secs_f64() * 1000.0);
        None
    }

    #[allow(dead_code)]
    pub async fn health_check(&self, peer: SocketAddr) -> bool {
        let pool = self.tcp_pool(peer).await;
        let session = match timeout(
            self.query_timeout,
            pool.acquire(|| async move {
                let stream = TcpStream::connect(peer).await?;
                stream.set_nodelay(true)?;
                Ok(MultiplexedSession::from_tcp(stream))
            }),
        )
        .await
        {
            Ok(Ok(session)) => session,
            Ok(Err(err)) => {
                debug!(peer = %peer, err = debug(err), "peer health check connect failed");
                return false;
            }
            Err(_) => return false,
        };
        let response = session
            .send_request(
                Request::whole_chunk(MessageType::HealthCheck, CheckSum::empty()),
                self.query_timeout,
            )
            .await;
        pool.prune().await;
        matches!(response, Ok(resp) if resp.status == super::super::STATUS_HIT)
    }

    async fn query_peer(
        &self,
        peer: SocketAddr,
        checksum: &CheckSum,
    ) -> io::Result<Option<Vec<u8>>> {
        let begin = Instant::now();
        let pool = self.tcp_pool(peer).await;
        let result = async {
            let session = timeout(
                self.query_timeout,
                pool.acquire(|| async move {
                    let stream = TcpStream::connect(peer).await?;
                    stream.set_nodelay(true)?;
                    Ok(MultiplexedSession::from_tcp(stream))
                }),
            )
            .await
            .map_err(|_| timeout_error("peer connect"))??;
            let response = session
                .send_request(
                    Request::whole_chunk(MessageType::GetChunk, *checksum),
                    self.query_timeout,
                )
                .await?;
            pool.prune().await;
            match response.status {
                super::super::STATUS_HIT => Ok(Some(response.payload)),
                super::super::STATUS_MISS => Ok(None),
                _ => Err(io::Error::other("peer returned error")),
            }
        }
        .await;
        let elapsed_ms = begin.elapsed().as_secs_f64() * 1000.0;
        let status = match &result {
            Ok(Some(_)) => "hit",
            Ok(None) => "miss",
            Err(err) if err.kind() == ErrorKind::TimedOut => "timeout",
            Err(_) => "error",
        };
        self.metrics.record_query(status, elapsed_ms);
        result
    }

    async fn tcp_pool(&self, peer: SocketAddr) -> Arc<TcpConnPool> {
        let mut pools = self.pools.lock().await;
        Arc::clone(
            pools
                .entry(peer)
                .or_insert_with(|| Arc::new(TcpConnPool::default())),
        )
    }
}
