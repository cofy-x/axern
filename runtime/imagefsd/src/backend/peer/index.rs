use super::metrics::ChunkIndexMetrics;
use super::{PeerRuntime, MAX_CHUNK_OWNERS, REDIS_REGISTER_BATCH_SIZE};
use crate::backend::chunkdb::{CheckSum, CheckSumOnDisk, ChunkIndexControl};
use crate::utils::now_epoch_secs;
use anyhow::Context;
use async_trait::async_trait;
use opentelemetry::global;
use redis::aio::MultiplexedConnection;
use std::collections::HashSet;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, RwLock};
use std::time::{Duration, Instant};
use tokio::sync::Mutex;
use tracing::{debug, warn};

#[async_trait]
pub trait ChunkIndex: Send + Sync {
    async fn lookup_owners(&self, cs: &CheckSum) -> anyhow::Result<Vec<SocketAddr>>;
    async fn register(&self, cs: &CheckSum) -> anyhow::Result<()>;
    async fn register_batch(&self, checksums: &[CheckSum]) -> anyhow::Result<()>;
    async fn unregister(&self, cs: &CheckSum) -> anyhow::Result<()>;
    fn refresh_interval(&self) -> Option<Duration> {
        None
    }
    async fn unregister_batch(&self, checksums: &[CheckSum]) -> anyhow::Result<()> {
        for checksum in checksums {
            self.unregister(checksum).await?;
        }
        Ok(())
    }
    async fn sync_existing_chunks(&self, checksums: &[CheckSum]) -> anyhow::Result<usize> {
        self.register_batch(checksums).await?;
        Ok(checksums.len())
    }
    async fn refresh_registered(&self, _spread_over: Duration) -> anyhow::Result<Option<usize>> {
        Ok(None)
    }
    async fn repair_missing_owners(
        &self,
        _checksums: &[CheckSum],
    ) -> anyhow::Result<Option<usize>> {
        Ok(None)
    }
}

fn chunk_index_key(cs: &CheckSum) -> String {
    format!("imagefsd:chunk-owner:{cs}")
}

const REDIS_CHUNK_REGISTER_SCRIPT: &str = r#"
local cutoff = tonumber(ARGV[2]) - tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff)
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[1])
local count = redis.call('ZCARD', KEYS[1])
local max_owners = tonumber(ARGV[4])
if count > max_owners then
    redis.call('ZREMRANGEBYRANK', KEYS[1], 0, count - max_owners - 1)
end
redis.call('EXPIRE', KEYS[1], ARGV[3])
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then
    return 1
end
return 0
"#;

#[derive(Debug, Default)]
pub(super) struct IndexTracker {
    registered: RwLock<HashSet<CheckSumOnDisk>>,
}

impl IndexTracker {
    pub(super) fn insert_many(&self, checksums: impl IntoIterator<Item = CheckSumOnDisk>) {
        let mut registered = self.registered.write().unwrap();
        registered.extend(checksums);
    }

    pub(super) fn remove_many(&self, checksums: impl IntoIterator<Item = CheckSumOnDisk>) {
        let mut registered = self.registered.write().unwrap();
        for checksum in checksums {
            registered.remove(&checksum);
        }
    }

    pub(super) fn contains(&self, checksum: &CheckSum) -> bool {
        self.registered
            .read()
            .unwrap()
            .contains(&CheckSumOnDisk::from(*checksum))
    }

    fn len(&self) -> usize {
        self.registered.read().unwrap().len()
    }

    pub(super) fn snapshot(&self) -> Vec<CheckSum> {
        let snapshot = self
            .registered
            .read()
            .unwrap()
            .iter()
            .copied()
            .collect::<Vec<_>>();
        snapshot.into_iter().map(CheckSum::from).collect()
    }
}

pub struct RedisChunkIndex {
    client: redis::Client,
    connection: Mutex<Option<MultiplexedConnection>>,
    register_script_loaded: AtomicBool,
    advertise_addr: SocketAddr,
    node_id: String,
    ttl_secs: u64,
    tracker: IndexTracker,
    register_script: redis::Script,
    metrics: ChunkIndexMetrics,
}

impl std::fmt::Debug for RedisChunkIndex {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("RedisChunkIndex")
            .field("advertise_addr", &self.advertise_addr)
            .field("node_id", &self.node_id)
            .field("ttl_secs", &self.ttl_secs)
            .finish()
    }
}

impl RedisChunkIndex {
    pub fn new(
        url: &str,
        advertise_addr: SocketAddr,
        node_id: &str,
        ttl_secs: u64,
    ) -> anyhow::Result<Self> {
        let meter = global::meter("imagefsd.chunk_index");
        let client = redis::Client::open(url).context("failed to create redis client")?;
        Ok(Self {
            client,
            connection: Mutex::new(None),
            register_script_loaded: AtomicBool::new(false),
            advertise_addr,
            node_id: node_id.to_string(),
            ttl_secs,
            tracker: IndexTracker::default(),
            register_script: redis::Script::new(REDIS_CHUNK_REGISTER_SCRIPT),
            metrics: ChunkIndexMetrics::new(&meter),
        })
    }

    async fn redis_conn(&self) -> anyhow::Result<MultiplexedConnection> {
        let mut guard = self.connection.lock().await;
        if let Some(conn) = guard.as_ref() {
            return Ok(conn.clone());
        }
        let conn = self
            .client
            .get_multiplexed_async_connection()
            .await
            .context("failed to connect to redis")?;
        self.register_script_loaded.store(false, Ordering::Relaxed);
        *guard = Some(conn.clone());
        Ok(conn)
    }

    async fn load_register_script(&self, conn: &mut MultiplexedConnection) -> anyhow::Result<()> {
        if self.register_script_loaded.load(Ordering::Relaxed) {
            return Ok(());
        }
        self.register_script
            .prepare_invoke()
            .load_async(conn)
            .await
            .context("failed to load redis chunk register script")?;
        self.register_script_loaded.store(true, Ordering::Relaxed);
        Ok(())
    }

    fn live_owner_cutoff(&self) -> i64 {
        now_epoch_secs().saturating_sub(self.ttl_secs) as i64
    }

    async fn lookup_owner_values(
        &self,
        checksums: &[CheckSum],
    ) -> anyhow::Result<Vec<Vec<String>>> {
        if checksums.is_empty() {
            return Ok(Vec::new());
        }
        let mut conn = self.redis_conn().await?;
        let cutoff = self.live_owner_cutoff();
        let mut owners = Vec::with_capacity(checksums.len());
        for batch in checksums.chunks(REDIS_REGISTER_BATCH_SIZE) {
            let mut pipe = redis::pipe();
            for checksum in batch {
                pipe.cmd("ZRANGEBYSCORE")
                    .arg(chunk_index_key(checksum))
                    .arg(cutoff)
                    .arg("+inf");
            }
            let values: Vec<redis::Value> = pipe.query_async(&mut conn).await?;
            for value in values {
                owners.push(redis::from_redis_value(&value)?);
            }
        }
        Ok(owners)
    }

    async fn resolve_owners(&self, cs: &CheckSum) -> anyhow::Result<Vec<SocketAddr>> {
        let begin = Instant::now();
        let result = async {
            let values = self
                .lookup_owner_values(std::slice::from_ref(cs))
                .await?
                .into_iter()
                .next()
                .unwrap_or_default();
            values
                .into_iter()
                .map(|value| value.parse::<SocketAddr>())
                .collect::<Result<Vec<_>, _>>()
                .map_err(anyhow::Error::from)
        }
        .await;
        let elapsed_ms = begin.elapsed().as_secs_f64() * 1000.0;
        match &result {
            Ok(owners) => {
                let status = if owners.is_empty() { "miss" } else { "hit" };
                self.metrics.record_lookup(status, elapsed_ms, owners.len());
            }
            Err(_) => {
                self.metrics.record_lookup("error", elapsed_ms, 0);
                self.metrics.record_error("lookup");
            }
        }
        result
    }

    async fn register_many(&self, checksums: &[CheckSum]) -> anyhow::Result<usize> {
        if checksums.is_empty() {
            return Ok(0);
        }
        let mode = if checksums.len() == 1 {
            "single"
        } else {
            "batch"
        };
        self.metrics
            .record_register_attempt(mode, checksums.len() as u64);
        let mut conn = match self.redis_conn().await {
            Ok(conn) => conn,
            Err(err) => {
                self.metrics.record_error("register");
                return Err(err);
            }
        };
        if let Err(err) = self.load_register_script(&mut conn).await {
            self.metrics.record_error("register");
            return Err(err);
        }
        let advertise_addr = self.advertise_addr.to_string();
        let now = now_epoch_secs() as i64;
        let ttl_secs = self.ttl_secs as i64;
        let mut retained = Vec::new();
        for batch in checksums.chunks(REDIS_REGISTER_BATCH_SIZE) {
            let mut pipe = redis::pipe();
            for checksum in batch {
                let key = chunk_index_key(checksum);
                let mut invocation = self.register_script.prepare_invoke();
                invocation
                    .key(&key)
                    .arg(&advertise_addr)
                    .arg(now)
                    .arg(ttl_secs)
                    .arg(MAX_CHUNK_OWNERS as i64);
                pipe.invoke_script(&invocation);
            }
            let results: Vec<i32> = match pipe.query_async(&mut conn).await {
                Ok(results) => results,
                Err(err) => {
                    self.metrics.record_error("register");
                    return Err(err.into());
                }
            };
            retained.extend(batch.iter().zip(results.into_iter()).filter_map(
                |(checksum, result)| (result == 1).then_some(CheckSumOnDisk::from(*checksum)),
            ));
        }
        let registered = retained.len();
        let dropped = checksums.len().saturating_sub(registered);
        self.tracker.insert_many(retained);
        if dropped > 0 {
            let tracker_size = self.tracker.len();
            if dropped * 4 >= checksums.len() {
                warn!(
                    dropped,
                    attempted = checksums.len(),
                    tracker_size,
                    advertise_addr = %self.advertise_addr,
                    "chunk index registration results were evicted before retention"
                );
            } else {
                debug!(
                    dropped,
                    attempted = checksums.len(),
                    tracker_size,
                    advertise_addr = %self.advertise_addr,
                    "chunk index registration results were evicted before retention"
                );
            }
        }
        self.metrics
            .record_register_success(mode, registered as u64);
        Ok(registered)
    }

    async fn unregister_many(&self, checksums: &[CheckSum]) -> anyhow::Result<()> {
        if checksums.is_empty() {
            return Ok(());
        }
        let mode = if checksums.len() == 1 {
            "single"
        } else {
            "batch"
        };
        let mut conn = match self.redis_conn().await {
            Ok(conn) => conn,
            Err(err) => {
                self.metrics.record_error("unregister");
                return Err(err);
            }
        };
        let advertise_addr = self.advertise_addr.to_string();
        for batch in checksums.chunks(REDIS_REGISTER_BATCH_SIZE) {
            let mut pipe = redis::pipe();
            for checksum in batch {
                let key = chunk_index_key(checksum);
                pipe.cmd("ZREM").arg(&key).arg(&advertise_addr).ignore();
            }
            if let Err(err) = pipe.query_async::<()>(&mut conn).await {
                self.metrics.record_error("unregister");
                return Err(err.into());
            }
        }
        self.tracker
            .remove_many(checksums.iter().copied().map(CheckSumOnDisk::from));
        self.metrics.record_unregister(mode, checksums.len() as u64);
        Ok(())
    }

    async fn sync_chunks(&self, checksums: &[CheckSum]) -> anyhow::Result<usize> {
        let owner_sets = self.lookup_owner_values(checksums).await?;
        let advertise_addr = self.advertise_addr.to_string();
        let mut tracked = Vec::new();
        let mut missing = Vec::new();
        for (checksum, owners) in checksums.iter().zip(owner_sets.into_iter()) {
            if owners.iter().any(|owner| owner == &advertise_addr) {
                tracked.push(CheckSumOnDisk::from(*checksum));
            } else if owners.len() < MAX_CHUNK_OWNERS {
                missing.push(*checksum);
            }
        }
        let tracked_count = tracked.len();
        self.tracker.insert_many(tracked);
        let registered = self.register_many(&missing).await?;
        Ok(tracked_count + registered)
    }

    pub(super) fn refresh_batch_spacing(spread_over: Duration, num_batches: usize) -> Duration {
        if num_batches <= 1 {
            Duration::ZERO
        } else {
            spread_over.mul_f64(0.9) / (num_batches as u32)
        }
    }

    async fn refresh_registrations(&self, spread_over: Duration) -> anyhow::Result<usize> {
        let checksums = self.tracker.snapshot();
        let total = checksums.len();
        if total == 0 {
            return Ok(0);
        }
        let num_batches = checksums.chunks(REDIS_REGISTER_BATCH_SIZE).len();
        let sleep_between = Self::refresh_batch_spacing(spread_over, num_batches);
        debug!(
            tracker_size = total,
            num_batches,
            sleep_ms = sleep_between.as_millis(),
            "refreshing tracked chunk index registrations (spread)"
        );
        let started = tokio::time::Instant::now();
        let mut registered = 0usize;
        for (index, batch) in checksums.chunks(REDIS_REGISTER_BATCH_SIZE).enumerate() {
            registered += self.register_many(batch).await?;
            if index + 1 < num_batches && !sleep_between.is_zero() {
                let next_batch_at = started + sleep_between.mul_f64((index + 1) as f64);
                tokio::time::sleep_until(next_batch_at).await;
            }
        }
        Ok(registered)
    }

    async fn repair_missing_chunks(&self, checksums: &[CheckSum]) -> anyhow::Result<usize> {
        let candidates = checksums
            .iter()
            .copied()
            .filter(|checksum| !self.tracker.contains(checksum))
            .collect::<Vec<_>>();
        self.sync_chunks(&candidates).await
    }
}

#[async_trait]
impl ChunkIndex for RedisChunkIndex {
    async fn lookup_owners(&self, cs: &CheckSum) -> anyhow::Result<Vec<SocketAddr>> {
        self.resolve_owners(cs).await
    }

    async fn register(&self, cs: &CheckSum) -> anyhow::Result<()> {
        self.register_many(&[*cs]).await.map(|_| ())
    }

    async fn register_batch(&self, checksums: &[CheckSum]) -> anyhow::Result<()> {
        self.register_many(checksums).await.map(|_| ())
    }

    async fn unregister(&self, cs: &CheckSum) -> anyhow::Result<()> {
        self.unregister_many(&[*cs]).await
    }

    fn refresh_interval(&self) -> Option<Duration> {
        Some(Duration::from_secs((self.ttl_secs / 3).max(1)))
    }

    async fn unregister_batch(&self, checksums: &[CheckSum]) -> anyhow::Result<()> {
        self.unregister_many(checksums).await
    }

    async fn sync_existing_chunks(&self, checksums: &[CheckSum]) -> anyhow::Result<usize> {
        self.sync_chunks(checksums).await
    }

    async fn refresh_registered(&self, spread_over: Duration) -> anyhow::Result<Option<usize>> {
        match self.refresh_registrations(spread_over).await {
            Ok(count) => {
                self.metrics.record_refresh("ok", count as u64);
                Ok(Some(count))
            }
            Err(err) => {
                self.metrics.record_error("refresh");
                Err(err)
            }
        }
    }

    async fn repair_missing_owners(&self, checksums: &[CheckSum]) -> anyhow::Result<Option<usize>> {
        match self.repair_missing_chunks(checksums).await {
            Ok(count) => {
                self.metrics.record_repair("ok", count as u64);
                Ok(Some(count))
            }
            Err(err) => {
                self.metrics.record_error("repair");
                Err(err)
            }
        }
    }
}

#[derive(Clone)]
pub struct SyncChunkIndexControl {
    runtime: PeerRuntime,
    chunk_index: Arc<dyn ChunkIndex>,
}

impl SyncChunkIndexControl {
    pub fn new(runtime: PeerRuntime, chunk_index: Arc<dyn ChunkIndex>) -> Self {
        Self {
            runtime,
            chunk_index,
        }
    }
}

impl ChunkIndexControl for SyncChunkIndexControl {
    fn register_chunk(&self, checksum: &CheckSum) -> bool {
        self.runtime
            .block_on(self.chunk_index.register(checksum))
            .is_ok()
    }

    fn register_chunks(&self, checksums: &[CheckSum]) -> bool {
        self.runtime
            .block_on(self.chunk_index.register_batch(checksums))
            .is_ok()
    }

    fn unregister_chunk(&self, checksum: &CheckSum) -> bool {
        self.runtime
            .block_on(self.chunk_index.unregister(checksum))
            .is_ok()
    }

    fn unregister_chunks(&self, checksums: &[CheckSum]) -> bool {
        self.runtime
            .block_on(self.chunk_index.unregister_batch(checksums))
            .is_ok()
    }
}
