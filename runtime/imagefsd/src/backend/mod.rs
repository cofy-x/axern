use crate::rate_limited_log;
use crate::utils::RateLimitedLog;
use fuse_backend_rs::api::filesystem::ZeroCopyWriter;
use heed::{Env, RwTxn};
use std::fmt::Debug;
use std::sync::Arc;
use std::time::{Duration, Instant};
pub mod cache;
pub mod chunkdb;
pub mod dedup;
pub mod general;
pub mod indexdb;
pub mod peer;

pub const CHUNK_SIZE: usize = 4 * 1024 * 1024_usize;
const WRITE_TXN_SLOW_THRESHOLD: Duration = Duration::from_secs(1);

static SLOW_WRITE_TXN_LOG: RateLimitedLog = RateLimitedLog::new(30);

pub(crate) fn slow_write_txn<'e>(env: &'e Env, caller: &str) -> heed::Result<RwTxn<'e>> {
    let t = Instant::now();
    let txn = env.write_txn()?;
    let elapsed = t.elapsed();
    if elapsed >= WRITE_TXN_SLOW_THRESHOLD {
        rate_limited_log!(
            SLOW_WRITE_TXN_LOG,
            tracing::warn!("{caller}: write_txn lock acquired in {elapsed:?}")
        );
    }
    Ok(txn)
}

pub trait Backend: Debug + Sync + Send {
    fn size(&self) -> u64;
    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize>;

    #[allow(dead_code)]
    fn write_to_fuse_writer(
        &self,
        off: usize,
        size: u32,
        w: &mut dyn ZeroCopyWriter,
    ) -> std::io::Result<usize> {
        let mut buf = vec![0_u8; size as usize];
        let n = self.fetch(off, &mut buf)?;
        w.write(&buf[..n])
    }
}

pub trait BackendEx: Backend {
    fn invalidate_chunk(&self, chunk_id: usize) -> std::io::Result<()>;
}

impl<T: Backend> Backend for Arc<T> {
    fn size(&self) -> u64 {
        (**self).size()
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        (**self).fetch(off, data)
    }
}

impl<T: BackendEx> BackendEx for Arc<T> {
    fn invalidate_chunk(&self, chunk_id: usize) -> std::io::Result<()> {
        (**self).invalidate_chunk(chunk_id)
    }
}
