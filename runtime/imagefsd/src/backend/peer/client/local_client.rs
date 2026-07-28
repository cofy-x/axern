use super::circuit_breaker::CircuitBreaker;
use crate::backend::chunkdb::{CheckSum, ChunkIndexControl};
use crate::backend::peer::metrics::LocalClientMetrics;
use crate::backend::peer::protocol::{MessageType, Request};
use crate::backend::peer::session::{MultiplexedSession, UnixConnPool};
use crate::backend::peer::{
    timeout_error, PeerRuntime, CIRCUIT_BREAKER_COOLDOWN, HEALTH_CHECK_INTERVAL,
};
use opentelemetry::global;
use serde::{Deserialize, Serialize};
use std::io::{self, ErrorKind};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::net::UnixStream;
use tokio::time::{interval, timeout};
use tracing::debug;

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct LocalityStats {
    pub chunkdb_total_chunks: u64,
    pub chunkdb_used_bytes: u64,
    pub chunkdb_recent_access_age_secs: u64,
    pub peer_healthy_count: u64,
    pub peer_unhealthy_count: u64,
    pub peer_hinted_count: u64,
}

#[derive(Debug, Clone)]
pub struct LocalChunkClient {
    runtime: PeerRuntime,
    pub(in crate::backend::peer) socket_path: PathBuf,
    timeout: Duration,
    pub(in crate::backend::peer) pool: Arc<UnixConnPool>,
    pub(in crate::backend::peer) metrics: LocalClientMetrics,
    pub(in crate::backend::peer) breaker: Arc<CircuitBreaker>,
}

impl LocalChunkClient {
    pub fn new<P: AsRef<Path>>(runtime: PeerRuntime, socket_path: P, timeout: Duration) -> Self {
        let meter = global::meter("imagefsd.local");
        Self {
            runtime,
            socket_path: socket_path.as_ref().to_path_buf(),
            timeout,
            pool: Arc::new(UnixConnPool::default()),
            metrics: LocalClientMetrics::new(&meter),
            breaker: Arc::new(CircuitBreaker::new(CIRCUIT_BREAKER_COOLDOWN)),
        }
    }

    pub fn start_health_checker(&self) {
        let socket_path = self.socket_path.clone();
        let timeout_dur = self.timeout;
        let breaker = Arc::clone(&self.breaker);
        let metrics = self.metrics.clone();
        self.runtime.spawn(async move {
            let mut tick = interval(HEALTH_CHECK_INTERVAL);
            loop {
                tick.tick().await;
                let result = timeout(timeout_dur, UnixStream::connect(&socket_path)).await;
                match result {
                    Ok(Ok(_stream)) => {
                        breaker.record_success();
                        metrics.record_request("health_check", "ok", 0.0);
                    }
                    _ => {
                        breaker.record_failure();
                        metrics.record_request("health_check", "unavailable", 0.0);
                    }
                }
            }
        });
    }

    pub fn prefetch_chunk_blocking(&self, checksum: &CheckSum) -> bool {
        self.runtime.block_on(self.prefetch_chunk(checksum))
    }

    #[allow(dead_code)]
    pub fn health_check_blocking(&self) -> bool {
        self.runtime.block_on(self.health_check())
    }

    pub fn stats_locality_blocking(&self) -> io::Result<LocalityStats> {
        self.runtime.block_on(self.stats_locality())
    }

    pub fn register_local_chunk(&self, checksum: &CheckSum) -> bool {
        self.runtime.block_on(self.control_request(
            "register",
            MessageType::RegisterChunk,
            checksum,
        ))
    }

    pub fn register_local_chunks(&self, checksums: &[CheckSum]) -> bool {
        self.runtime.block_on(self.control_batch_request(
            "register_batch",
            MessageType::RegisterChunks,
            checksums,
        ))
    }

    pub fn unregister_local_chunk(&self, checksum: &CheckSum) -> bool {
        self.runtime.block_on(self.control_request(
            "unregister",
            MessageType::UnregisterChunk,
            checksum,
        ))
    }

    pub fn unregister_local_chunks(&self, checksums: &[CheckSum]) -> bool {
        self.runtime.block_on(self.control_batch_request(
            "unregister_batch",
            MessageType::UnregisterChunks,
            checksums,
        ))
    }

    pub async fn prefetch_chunk(&self, checksum: &CheckSum) -> bool {
        self.control_request("prefetch", MessageType::PrefetchChunk, checksum)
            .await
    }

    #[allow(dead_code)]
    pub async fn health_check(&self) -> bool {
        self.control_request("health_check", MessageType::HealthCheck, &CheckSum::empty())
            .await
    }

    pub async fn stats_locality(&self) -> io::Result<LocalityStats> {
        if self.breaker.should_reject() {
            self.metrics
                .record_request("stats_locality", "circuit_open", 0.0);
            return Err(io::Error::other(
                "local chunk socket circuit breaker is open",
            ));
        }
        let begin = Instant::now();
        let result = self.send_stats_locality().await;
        let status = match &result {
            Ok(_) => "hit",
            Err(err) if err.kind() == ErrorKind::TimedOut => "timeout",
            Err(_) => "error",
        };
        self.metrics.record_request(
            "stats_locality",
            status,
            begin.elapsed().as_secs_f64() * 1000.0,
        );
        if result.is_ok() {
            self.breaker.record_success();
        } else {
            self.breaker.record_failure();
        }
        result
    }

    async fn control_request(
        &self,
        op: &'static str,
        message_type: MessageType,
        checksum: &CheckSum,
    ) -> bool {
        if self.breaker.should_reject() {
            self.metrics.record_request(op, "circuit_open", 0.0);
            return false;
        }
        let begin = Instant::now();
        let result = self.send_control(message_type, checksum).await;
        let status = match &result {
            Ok(true) => "hit",
            Ok(false) => "miss",
            Err(err) if err.kind() == ErrorKind::TimedOut => "timeout",
            Err(_) => "error",
        };
        self.metrics
            .record_request(op, status, begin.elapsed().as_secs_f64() * 1000.0);
        if result.is_ok() {
            self.breaker.record_success();
        } else {
            self.breaker.record_failure();
        }
        matches!(result, Ok(true))
    }

    async fn control_batch_request(
        &self,
        op: &'static str,
        message_type: MessageType,
        checksums: &[CheckSum],
    ) -> bool {
        if self.breaker.should_reject() {
            self.metrics.record_request(op, "circuit_open", 0.0);
            return false;
        }
        let begin = Instant::now();
        let result = self.send_control_batch(message_type, checksums).await;
        let status = match &result {
            Ok(true) => "hit",
            Ok(false) => "miss",
            Err(err) if err.kind() == ErrorKind::TimedOut => "timeout",
            Err(_) => "error",
        };
        self.metrics
            .record_request(op, status, begin.elapsed().as_secs_f64() * 1000.0);
        if result.is_ok() {
            self.breaker.record_success();
        } else {
            self.breaker.record_failure();
        }
        matches!(result, Ok(true))
    }

    async fn send_control(
        &self,
        message_type: MessageType,
        checksum: &CheckSum,
    ) -> io::Result<bool> {
        let session = match timeout(
            self.timeout,
            self.pool.acquire(|| async {
                let stream = UnixStream::connect(&self.socket_path).await?;
                Ok(MultiplexedSession::from_unix(stream))
            }),
        )
        .await
        {
            Ok(Ok(session)) => session,
            Ok(Err(err)) => {
                debug!(
                    sock = display(self.socket_path.display()),
                    err = debug(&err),
                    "local chunk socket unavailable"
                );
                return Err(err);
            }
            Err(_) => return Err(timeout_error("local connect")),
        };
        let response = session
            .send_request(Request::whole_chunk(message_type, *checksum), self.timeout)
            .await;
        self.pool.prune().await;
        let response = response?;
        match response.status {
            super::super::STATUS_HIT => Ok(true),
            super::super::STATUS_MISS => Ok(false),
            _ => Err(io::Error::other("local chunk server returned error")),
        }
    }

    async fn send_control_batch(
        &self,
        message_type: MessageType,
        checksums: &[CheckSum],
    ) -> io::Result<bool> {
        if checksums.is_empty() {
            return Ok(true);
        }
        let session = match timeout(
            self.timeout,
            self.pool.acquire(|| async {
                let stream = UnixStream::connect(&self.socket_path).await?;
                Ok(MultiplexedSession::from_unix(stream))
            }),
        )
        .await
        {
            Ok(Ok(session)) => session,
            Ok(Err(err)) => {
                debug!(
                    sock = display(self.socket_path.display()),
                    err = debug(&err),
                    "local chunk socket unavailable"
                );
                return Err(err);
            }
            Err(_) => return Err(timeout_error("local connect")),
        };
        let mut payload = Vec::with_capacity(checksums.len() * 33);
        for checksum in checksums {
            payload.push(checksum.method.into());
            payload.extend_from_slice(&checksum.raw);
        }
        let request = Request::control_batch(message_type, checksums.len());
        let response = session
            .send_batch_request(request, payload, self.timeout)
            .await;
        self.pool.prune().await;
        let response = response?;
        match response.status {
            super::super::STATUS_HIT => Ok(true),
            super::super::STATUS_MISS => Ok(false),
            _ => Err(io::Error::other("local chunk server returned error")),
        }
    }

    async fn send_stats_locality(&self) -> io::Result<LocalityStats> {
        let session = match timeout(
            self.timeout,
            self.pool.acquire(|| async {
                let stream = UnixStream::connect(&self.socket_path).await?;
                Ok(MultiplexedSession::from_unix(stream))
            }),
        )
        .await
        {
            Ok(Ok(session)) => session,
            Ok(Err(err)) => {
                debug!(
                    sock = display(self.socket_path.display()),
                    err = debug(&err),
                    "local chunk socket unavailable"
                );
                return Err(err);
            }
            Err(_) => return Err(timeout_error("local connect")),
        };
        let response = session
            .send_request(
                Request::whole_chunk(MessageType::StatsLocality, CheckSum::empty()),
                self.timeout,
            )
            .await;
        self.pool.prune().await;
        let response = response?;
        match response.status {
            super::super::STATUS_HIT => {
                serde_json::from_slice(&response.payload).map_err(io::Error::other)
            }
            super::super::STATUS_MISS => Err(io::Error::new(
                ErrorKind::NotFound,
                "local chunk server returned miss",
            )),
            _ => Err(io::Error::other("local chunk server returned error")),
        }
    }
}

impl ChunkIndexControl for LocalChunkClient {
    fn register_chunk(&self, checksum: &CheckSum) -> bool {
        self.register_local_chunk(checksum)
    }

    fn register_chunks(&self, checksums: &[CheckSum]) -> bool {
        self.register_local_chunks(checksums)
    }

    fn unregister_chunk(&self, checksum: &CheckSum) -> bool {
        self.unregister_local_chunk(checksum)
    }

    fn unregister_chunks(&self, checksums: &[CheckSum]) -> bool {
        self.unregister_local_chunks(checksums)
    }
}
