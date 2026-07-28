use anyhow::Context;
use std::future::Future;
use std::io::{self, ErrorKind};
#[cfg(all(test, target_os = "linux"))]
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::sync::OnceLock;
use std::time::{Duration, Instant};
#[cfg(all(test, target_os = "linux"))]
use tokio::io::split;
use tokio::runtime::{Builder, Runtime};
use tokio::sync::Notify;
use tokio::task::JoinHandle;

#[cfg(all(test, target_os = "linux"))]
use crate::backend::chunkdb::{CheckSum, CheckSumOnDisk, ChunkDB};

mod client;
mod discovery;
mod index;
mod metrics;
mod protocol;
mod server;
mod session;

#[cfg(all(test, target_os = "linux"))]
use client::CircuitBreaker;
#[cfg(all(test, target_os = "linux"))]
use index::IndexTracker;
#[cfg(all(test, target_os = "linux"))]
use metrics::{ChunkIndexMetrics, ChunkServerMetrics, LocalClientMetrics, PeerClientMetrics};
#[cfg(all(test, target_os = "linux"))]
use protocol::WireResponse;
#[cfg(all(test, target_os = "linux"))]
use session::{ConnectionPoolConfig, MultiplexedSession, TcpConnPool, UnixConnPool};

pub use client::{LocalChunkClient, LocalityStats, PeerClient, PeerHealthTracker, PeerHitHints};
pub use discovery::{
    build_discovery, parse_peer_addrs, PeerDiscovery, RedisDiscovery, StaticPeers,
};
pub use index::{ChunkIndex, RedisChunkIndex, SyncChunkIndexControl};
pub use protocol::{MessageType, Request};
pub use server::{default_chunk_server_socket, default_node_id, ChunkServer};

const REQUEST_LEN: usize = 50;
const RESPONSE_HEADER_LEN: usize = 13;
const STATUS_HIT: u8 = 0;
const STATUS_MISS: u8 = 1;
const STATUS_ERROR: u8 = 2;
const DEFAULT_TIMEOUT_MS: u64 = 1000;
const DEFAULT_MAX_QUERY_PEERS: usize = 3;
const HINT_MAX_RECENT: usize = 256;
const HINT_TTL_SECS: u64 = 300;
const HINT_GC_INTERVAL_SECS: u64 = 30;
const MAX_CHUNK_PAYLOAD_SIZE: usize = super::CHUNK_SIZE;
const DEFAULT_MAX_CONNECTIONS: usize = 8;
const DISCOVERY_TTL_SECS: u64 = 60;
const DISCOVERY_REFRESH_SECS: u64 = 20;
const REDIS_REGISTER_BATCH_SIZE: usize = 1000;
const INDEX_SYNC_BATCH_SIZE: usize = 1000;
const INDEX_REPAIR_BATCH_SIZE: usize = 256;
const MAX_CHUNK_OWNERS: usize = 3;
const CONNECTION_POOL_MIN_IDLE: usize = 1;
const CONNECTION_POOL_MAX_SIZE: usize = 8;
const CONNECTION_POOL_IDLE_TTL_SECS: u64 = 30;
const SERVER_KEEPALIVE_IDLE_TIMEOUT_SECS: u64 = 30;
const SESSION_MAX_INFLIGHT: usize = 32;
const CIRCUIT_BREAKER_COOLDOWN: Duration = Duration::from_secs(30);
const HEALTH_CHECK_INTERVAL: Duration = Duration::from_secs(30);

fn session_clock_base() -> Instant {
    static BASE: OnceLock<Instant> = OnceLock::new();
    *BASE.get_or_init(Instant::now)
}

fn monotonic_now_micros() -> u64 {
    session_clock_base().elapsed().as_micros() as u64
}

fn instant_from_micros(micros: u64) -> Instant {
    session_clock_base() + Duration::from_micros(micros)
}

fn timeout_error(op: &str) -> io::Error {
    io::Error::new(ErrorKind::TimedOut, format!("{op} timed out"))
}

#[derive(Clone, Debug)]
pub struct PeerRuntime {
    runtime: Arc<Runtime>,
}

impl PeerRuntime {
    pub fn new() -> anyhow::Result<Self> {
        Self::new_with_worker_threads(8)
    }

    pub fn new_with_worker_threads(worker_threads: usize) -> anyhow::Result<Self> {
        let runtime = Builder::new_multi_thread()
            .worker_threads(worker_threads)
            .max_blocking_threads(16)
            .thread_keep_alive(Duration::from_secs(300))
            .enable_io()
            .enable_time()
            .build()
            .context("failed to build tokio runtime")?;
        Ok(Self {
            runtime: Arc::new(runtime),
        })
    }

    pub fn block_on<F: Future>(&self, future: F) -> F::Output {
        self.runtime.block_on(future)
    }

    pub fn spawn<F>(&self, future: F) -> JoinHandle<F::Output>
    where
        F: Future + Send + 'static,
        F::Output: Send + 'static,
    {
        self.runtime.spawn(future)
    }
}

#[derive(Debug, Clone)]
pub struct ShutdownHandle {
    state: Arc<ShutdownState>,
}

#[derive(Debug)]
struct ShutdownState {
    stopped: AtomicBool,
    notify: Notify,
}

impl ShutdownHandle {
    fn new() -> Self {
        Self {
            state: Arc::new(ShutdownState {
                stopped: AtomicBool::new(false),
                notify: Notify::new(),
            }),
        }
    }

    pub fn shutdown(&self) {
        self.state.stopped.store(true, Ordering::Relaxed);
        self.state.notify.notify_waiters();
    }

    fn is_shutdown(&self) -> bool {
        self.state.stopped.load(Ordering::Relaxed)
    }

    async fn wait(&self) {
        if self.is_shutdown() {
            return;
        }
        self.state.notify.notified().await;
    }
}

#[cfg(all(test, target_os = "linux"))]
mod tests;
