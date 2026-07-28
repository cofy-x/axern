#![allow(dead_code)]

use async_trait::async_trait;
use imagefsd::backend::chunkdb::CheckSum;
#[cfg(feature = "redis-integration-tests")]
use imagefsd::backend::chunkdb::CheckSumMethod;
use imagefsd::backend::peer::ChunkIndex;
#[cfg(feature = "redis-integration-tests")]
use redis::Commands;
use std::net::SocketAddr;
#[cfg(feature = "redis-integration-tests")]
use std::process;
#[cfg(feature = "redis-integration-tests")]
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
#[cfg(feature = "redis-integration-tests")]
use std::sync::{MutexGuard, OnceLock};
use std::thread;
use std::time::{Duration, Instant};

pub fn wait_until<F>(cond: F)
where
    F: FnMut() -> bool,
{
    wait_until_with_timeout(Duration::from_secs(3), cond);
}

pub fn wait_until_with_timeout<F>(timeout: Duration, mut cond: F)
where
    F: FnMut() -> bool,
{
    let start = Instant::now();
    while start.elapsed() < timeout {
        if cond() {
            return;
        }
        thread::sleep(Duration::from_millis(20));
    }
    panic!("condition not met before timeout");
}

#[cfg(feature = "redis-integration-tests")]
pub fn checksum_for(label: &str) -> CheckSum {
    CheckSum::from_data(label.as_bytes(), CheckSumMethod::Blake3)
}

#[derive(Debug, Default)]
pub struct TestChunkIndex {
    pub owners: Vec<SocketAddr>,
    registered: Arc<Mutex<Vec<CheckSum>>>,
    unregistered: Arc<Mutex<Vec<CheckSum>>>,
}

impl TestChunkIndex {
    pub fn with_owners(owners: Vec<SocketAddr>) -> Self {
        Self {
            owners,
            ..Default::default()
        }
    }

    pub fn registered(&self) -> Vec<CheckSum> {
        self.registered.lock().unwrap().clone()
    }

    pub fn unregistered(&self) -> Vec<CheckSum> {
        self.unregistered.lock().unwrap().clone()
    }
}

#[async_trait]
impl ChunkIndex for TestChunkIndex {
    async fn lookup_owners(&self, _cs: &CheckSum) -> anyhow::Result<Vec<SocketAddr>> {
        Ok(self.owners.clone())
    }

    async fn register(&self, cs: &CheckSum) -> anyhow::Result<()> {
        self.registered.lock().unwrap().push(*cs);
        Ok(())
    }

    async fn register_batch(&self, checksums: &[CheckSum]) -> anyhow::Result<()> {
        self.registered.lock().unwrap().extend_from_slice(checksums);
        Ok(())
    }

    async fn unregister(&self, cs: &CheckSum) -> anyhow::Result<()> {
        self.unregistered.lock().unwrap().push(*cs);
        Ok(())
    }
}

#[cfg(feature = "redis-integration-tests")]
static REDIS_TEST_MUTEX: OnceLock<Mutex<()>> = OnceLock::new();
#[cfg(feature = "redis-integration-tests")]
static REDIS_TEST_ID: AtomicUsize = AtomicUsize::new(1);
#[cfg(feature = "redis-integration-tests")]
const DEFAULT_REDIS_TEST_URL: &str = "redis://imagefsd-redis:6379/15";

#[cfg(feature = "redis-integration-tests")]
fn redis_test_url() -> String {
    std::env::var("DISTILL_FS_TEST_REDIS_URL")
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| DEFAULT_REDIS_TEST_URL.to_string())
}

#[cfg(feature = "redis-integration-tests")]
fn try_flush_redis(url: &str) -> redis::RedisResult<()> {
    let client = redis::Client::open(url).unwrap();
    let mut conn = client.get_connection().unwrap();
    redis::cmd("FLUSHDB").query::<()>(&mut conn)
}

#[cfg(feature = "redis-integration-tests")]
pub struct RedisTestGuard {
    url: String,
    _guard: MutexGuard<'static, ()>,
}

#[cfg(feature = "redis-integration-tests")]
impl RedisTestGuard {
    pub fn acquire() -> Self {
        let url = redis_test_url();
        let guard = REDIS_TEST_MUTEX
            .get_or_init(|| Mutex::new(()))
            .lock()
            .unwrap_or_else(|err| err.into_inner());
        try_flush_redis(&url).unwrap();
        Self { url, _guard: guard }
    }

    pub fn url(&self) -> &str {
        &self.url
    }
}

#[cfg(feature = "redis-integration-tests")]
impl Drop for RedisTestGuard {
    fn drop(&mut self) {
        let _ = try_flush_redis(&self.url);
    }
}

#[cfg(feature = "redis-integration-tests")]
pub fn unique_node_id(prefix: &str) -> String {
    format!(
        "{prefix}-{}-{}",
        process::id(),
        REDIS_TEST_ID.fetch_add(1, Ordering::Relaxed)
    )
}

#[cfg(feature = "redis-integration-tests")]
pub fn wait_for_next_epoch_second() {
    let start = imagefsd::utils::now_epoch_secs();
    wait_until(|| imagefsd::utils::now_epoch_secs() > start);
}

#[cfg(feature = "redis-integration-tests")]
pub fn redis_set_string(url: &str, key: &str, value: &str, ttl_secs: u64) {
    let client = redis::Client::open(url).unwrap();
    let mut conn = client.get_connection().unwrap();
    let _: () = conn.set_ex(key, value, ttl_secs).unwrap();
}

#[cfg(feature = "redis-integration-tests")]
pub fn chunk_index_key(checksum: &CheckSum) -> String {
    format!("imagefsd:chunk-owner:{checksum}")
}

#[cfg(feature = "redis-integration-tests")]
pub fn redis_remove_owner(url: &str, checksum: &CheckSum, owner: SocketAddr) {
    let client = redis::Client::open(url).unwrap();
    let mut conn = client.get_connection().unwrap();
    let _: usize = conn
        .zrem(chunk_index_key(checksum), owner.to_string())
        .unwrap();
}
