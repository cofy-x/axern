mod gc;
mod metrics;
mod types;
mod writer;

use super::slow_write_txn;
use crate::utils::now_epoch_secs;
use heed::{Database, Env, EnvOpenOptions, RwTxn};
use heed_types::Bytes;
use opentelemetry::global;
use serde_json::{json, Value};
use std::ops::Bound::{Excluded, Unbounded};
use std::path::Path;
use std::sync::{mpsc, Arc};
use std::thread;
use std::time::{Duration, Instant};
use tracing::{info, warn};

pub use self::gc::GcWorker;
use self::metrics::ChunkDbMetrics;
pub(crate) use self::types::CheckSumOnDisk;
use self::types::{AccessKey, AccessTime};
pub use self::types::{CheckSum, CheckSumMethod, GcDeleteResult};
use self::writer::{WriteRequest, WriterChannel, WriterThread};

pub const CHUNK_DB_NAME: &str = "data";
const ACCESS_DB_NAME: &str = "chunk_access";
const ACCESS_INDEX_DB_NAME: &str = "chunk_access_index";
pub const MAX_DBS: u32 = 16;
pub const LMDB_MAX_READERS: u32 = 4096;
#[cfg(all(target_os = "linux", not(test)))]
pub const CHUNK_DB_SIZE: usize = 100 * 1024 * 1024 * 1024_usize;
#[cfg(any(test, not(target_os = "linux")))]
pub const CHUNK_DB_SIZE: usize = 512 * 1024 * 1024_usize;
const GC_BATCH: usize = 128;
const ACCESS_BATCH_SIZE: usize = 1024;
const ADD_CHUNKS_BATCH_SIZE: usize = 16;
const ACCESS_REFRESH_INTERVAL_SECS: u64 = 300;
const ACCESS_UPDATE_MIN_INTERVAL_SECS: u64 = 60;
const DEFAULT_GC_EXPIRE_SECS: u64 = 24 * 60 * 60;
const GC_HIGH_WATERMARK: f64 = 0.90;
const GC_LOW_WATERMARK: f64 = 0.80;

pub trait ChunkIndexControl: Send + Sync {
    fn register_chunk(&self, checksum: &CheckSum) -> bool;
    fn register_chunks(&self, checksums: &[CheckSum]) -> bool {
        checksums
            .iter()
            .all(|checksum| self.register_chunk(checksum))
    }
    fn unregister_chunk(&self, checksum: &CheckSum) -> bool;
    fn unregister_chunks(&self, checksums: &[CheckSum]) -> bool {
        checksums
            .iter()
            .all(|checksum| self.unregister_chunk(checksum))
    }
}

type ChunkDataDb = Database<CheckSumOnDisk, Bytes>;
type ChunkAccessDb = Database<CheckSumOnDisk, AccessTime>;
type ChunkAccessIndexDb = Database<AccessKey, Bytes>;

pub struct ChunkDB {
    env: Env,
    data_db: ChunkDataDb,
    #[allow(dead_code)]
    access_db: ChunkAccessDb,
    access_index: ChunkAccessIndexDb,
    #[allow(dead_code)]
    index_ctl: Option<Arc<dyn ChunkIndexControl>>,
    metrics: ChunkDbMetrics,
    writer_channel: Arc<WriterChannel>,
    writer_handle: Option<thread::JoinHandle<()>>,
}

impl std::fmt::Debug for ChunkDB {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ChunkDB").finish_non_exhaustive()
    }
}

impl ChunkDB {
    pub fn new<P: AsRef<Path>>(path: P) -> anyhow::Result<Self> {
        Self::new_with_index_ctl(path, None)
    }

    pub fn new_with_index_ctl<P: AsRef<Path>>(
        path: P,
        index_ctl: Option<Arc<dyn ChunkIndexControl>>,
    ) -> anyhow::Result<Self> {
        let meter = global::meter("imagefsd.chunkdb");
        let open_begin = Instant::now();
        let env = unsafe {
            EnvOpenOptions::new()
                .map_size(CHUNK_DB_SIZE)
                .max_readers(LMDB_MAX_READERS)
                .max_dbs(MAX_DBS)
                .open(path)?
        };
        let stale_readers = env.clear_stale_readers()?;
        let actual_max_readers = env.max_readers();
        info!(
            max_readers = actual_max_readers,
            stale_readers_cleared = stale_readers,
            elapsed = ?open_begin.elapsed(),
            "ChunkDB opened LMDB environment"
        );
        if actual_max_readers < LMDB_MAX_READERS {
            warn!(
                requested_max_readers = LMDB_MAX_READERS,
                actual_max_readers,
                "ChunkDB LMDB max_readers is lower than requested; restart all processes sharing this env to apply the new limit."
            );
        }
        let (data_db, access_db, access_index) = Self::open_or_create_databases(&env)?;
        let writer_channel = Arc::new(WriterChannel::default());
        let writer_handle = Self::spawn_writer_thread(
            env.clone(),
            data_db,
            access_db,
            access_index,
            index_ctl.clone(),
            Arc::clone(&writer_channel),
        );
        Ok(Self {
            env,
            data_db,
            access_db,
            access_index,
            index_ctl,
            metrics: ChunkDbMetrics::new(&meter),
            writer_channel,
            writer_handle: Some(writer_handle),
        })
    }

    fn open_or_create_databases(
        env: &Env,
    ) -> anyhow::Result<(ChunkDataDb, ChunkAccessDb, ChunkAccessIndexDb)> {
        let t = std::time::Instant::now();
        let rtxn = env.read_txn()?;
        let data_db = env.open_database::<CheckSumOnDisk, Bytes>(&rtxn, Some(CHUNK_DB_NAME))?;
        let access_db =
            env.open_database::<CheckSumOnDisk, AccessTime>(&rtxn, Some(ACCESS_DB_NAME))?;
        let access_index =
            env.open_database::<AccessKey, Bytes>(&rtxn, Some(ACCESS_INDEX_DB_NAME))?;
        rtxn.commit()?;

        if let (Some(data_db), Some(access_db), Some(access_index)) =
            (data_db, access_db, access_index)
        {
            info!(
                "ChunkDB opened existing databases via read txn in {:?}",
                t.elapsed()
            );
            return Ok((data_db, access_db, access_index));
        }

        info!("ChunkDB databases not found, creating via write txn");
        let mut wtxn = slow_write_txn(env, "ChunkDB::open_or_create_databases")?;
        let data_db = env.create_database(&mut wtxn, Some(CHUNK_DB_NAME))?;
        let access_db = env.create_database(&mut wtxn, Some(ACCESS_DB_NAME))?;
        let access_index = env.create_database(&mut wtxn, Some(ACCESS_INDEX_DB_NAME))?;
        wtxn.commit()?;
        info!(
            "ChunkDB created databases via write txn in {:?}",
            t.elapsed()
        );
        Ok((data_db, access_db, access_index))
    }

    pub fn clear_stale_readers(&self) -> anyhow::Result<usize> {
        Ok(self.env.clear_stale_readers()?)
    }

    pub fn has_chunk(&self, cs: &CheckSum) -> anyhow::Result<bool> {
        let rtxn = self.env.read_txn()?;
        let cs_on_disk = CheckSumOnDisk::from(*cs);
        Ok(self.data_db.get(&rtxn, &cs_on_disk)?.is_some())
    }

    pub fn add_chunk(&self, cs: &CheckSum, data: Vec<u8>) -> anyhow::Result<()> {
        let data_len = data.len() as u64;
        let (tx, rx) = mpsc::sync_channel(1);
        self.writer_channel.submit_high(WriteRequest::AddChunk {
            cs: *cs,
            data,
            reply: tx,
        });
        let result: anyhow::Result<()> = rx
            .recv()
            .map_err(|_| anyhow::anyhow!("Writer thread dropped"))?;
        let status = if result.is_ok() { "ok" } else { "error" };
        self.metrics
            .record_add(status, 1, if result.is_ok() { data_len } else { 0 });
        result
    }

    pub fn add_chunks(&self, chunks: Vec<(CheckSum, Vec<u8>)>) -> anyhow::Result<()> {
        if chunks.is_empty() {
            return Ok(());
        }
        let total_bytes: u64 = chunks.iter().map(|(_, data)| data.len() as u64).sum();
        let chunk_count = chunks.len() as u64;

        let (tx, rx) = mpsc::sync_channel(1);
        self.writer_channel
            .submit_high(WriteRequest::AddChunksBatch { chunks, reply: tx });
        let result: anyhow::Result<()> = rx
            .recv()
            .map_err(|_| anyhow::anyhow!("Writer thread dropped"))?;
        let status = if result.is_ok() { "ok" } else { "error" };
        self.metrics.record_add(
            status,
            chunk_count,
            if result.is_ok() { total_bytes } else { 0 },
        );
        result
    }

    pub fn with_chunk<F, T>(&self, cs: &CheckSum, f: F) -> anyhow::Result<Option<T>>
    where
        F: FnOnce(&[u8]) -> std::io::Result<T>,
    {
        let begin = Instant::now();
        let rtxn = self.env.read_txn()?;
        let cs_on_disk = CheckSumOnDisk::from(*cs);
        let result = match self.data_db.get(&rtxn, &cs_on_disk)? {
            Some(chunk) => {
                let len = chunk.len();
                let res = f(chunk)?;
                drop(rtxn);
                self.enqueue_access_update(cs_on_disk);
                Ok(Some((res, len)))
            }
            None => Ok(None),
        };
        match result {
            Ok(Some((value, bytes))) => {
                self.metrics
                    .record_get("hit", begin.elapsed().as_secs_f64() * 1000.0, bytes);
                Ok(Some(value))
            }
            Ok(None) => {
                self.metrics
                    .record_get("miss", begin.elapsed().as_secs_f64() * 1000.0, 0);
                Ok(None)
            }
            Err(err) => {
                self.metrics
                    .record_get("error", begin.elapsed().as_secs_f64() * 1000.0, 0);
                Err(err)
            }
        }
    }

    pub fn with_chunk_range<F, T>(
        &self,
        cs: &CheckSum,
        start: usize,
        len: usize,
        f: F,
    ) -> anyhow::Result<Option<T>>
    where
        F: FnOnce(&[u8]) -> std::io::Result<T>,
    {
        let begin = Instant::now();
        let rtxn = self.env.read_txn()?;
        let cs_on_disk = CheckSumOnDisk::from(*cs);
        let result = match self.data_db.get(&rtxn, &cs_on_disk)? {
            Some(b) => {
                let end = start + len;
                if end > b.len() {
                    let err = Err(std::io::Error::new(
                        std::io::ErrorKind::InvalidData,
                        "Chunk range invalid",
                    )
                    .into());
                    drop(rtxn);
                    self.metrics
                        .record_get("error", begin.elapsed().as_secs_f64() * 1000.0, 0);
                    return err;
                }
                let res = f(&b[start..end])?;
                drop(rtxn);
                self.enqueue_access_update(cs_on_disk);
                Ok(Some((res, len)))
            }
            None => Ok(None),
        };
        match result {
            Ok(Some((value, bytes))) => {
                self.metrics
                    .record_get("hit", begin.elapsed().as_secs_f64() * 1000.0, bytes);
                Ok(Some(value))
            }
            Ok(None) => {
                self.metrics
                    .record_get("miss", begin.elapsed().as_secs_f64() * 1000.0, 0);
                Ok(None)
            }
            Err(err) => {
                self.metrics
                    .record_get("error", begin.elapsed().as_secs_f64() * 1000.0, 0);
                Err(err)
            }
        }
    }

    pub fn get_chunk(&self, cs: &CheckSum) -> anyhow::Result<Option<Vec<u8>>> {
        self.with_chunk(cs, |chunk| Ok(chunk.to_vec()))
    }

    #[cfg(test)]
    #[allow(dead_code)]
    pub fn list_all_chunks(&self) -> anyhow::Result<Vec<CheckSum>> {
        let mut checksums = Vec::new();
        let mut cursor = None;
        loop {
            let batch = self.next_chunk_batch(cursor, 1024)?;
            if batch.is_empty() {
                return Ok(checksums);
            }
            cursor = batch.last().copied();
            checksums.extend_from_slice(&batch);
        }
    }

    pub fn next_chunk_batch(
        &self,
        cursor: Option<CheckSum>,
        batch_size: usize,
    ) -> anyhow::Result<Vec<CheckSum>> {
        let batch_size = batch_size.max(1);
        let rtxn = self.env.read_txn()?;
        let mut batch = Vec::with_capacity(batch_size);
        match cursor.map(CheckSumOnDisk::from) {
            Some(last) => {
                let range = (Excluded(last), Unbounded);
                for item in self.data_db.range(&rtxn, &range)?.lazily_decode_data() {
                    let (cs, _) = item?;
                    if cs.is_valid() {
                        batch.push((*cs).into());
                        if batch.len() >= batch_size {
                            break;
                        }
                    }
                }
            }
            None => {
                for item in self.data_db.iter(&rtxn)?.lazily_decode_data() {
                    let (cs, _) = item?;
                    if cs.is_valid() {
                        batch.push((*cs).into());
                        if batch.len() >= batch_size {
                            break;
                        }
                    }
                }
            }
        }
        Ok(batch)
    }

    pub fn gc_expired(&self, timeout: Duration) -> anyhow::Result<GcDeleteResult> {
        let begin = Instant::now();
        let cutoff = now_epoch_secs().saturating_sub(timeout.as_secs());
        let rtxn = self.env.read_txn()?;
        let iter = self.access_index.iter(&rtxn)?;
        let mut expired = Vec::new();
        for it in iter {
            let (key, _) = it?;
            if key.last_access >= cutoff {
                break;
            }
            expired.push(key);
        }
        drop(rtxn);

        let mut total_removed = 0;
        let mut all_checksums = Vec::new();
        for batch in expired.chunks(GC_BATCH) {
            let result = self.delete_keys(batch.to_vec())?;
            total_removed += result.removed;
            all_checksums.extend(result.checksums);
        }

        let result = GcDeleteResult {
            removed: total_removed,
            checksums: all_checksums,
        };
        self.metrics.record_gc(
            "expired",
            "ok",
            begin.elapsed().as_secs_f64() * 1000.0,
            result.removed,
        );
        Ok(result)
    }

    pub fn gc_lru(&self, max_delete: usize) -> anyhow::Result<GcDeleteResult> {
        let begin = Instant::now();
        let rtxn = self.env.read_txn()?;
        let iter = self.access_index.iter(&rtxn)?;
        let mut targets = Vec::new();
        for it in iter {
            let (key, _) = it?;
            targets.push(key);
            if targets.len() >= max_delete {
                break;
            }
        }
        drop(rtxn);
        let result = self.delete_keys(targets);
        let (status, removed) = match &result {
            Ok(out) => ("ok", out.removed),
            Err(_) => ("error", 0),
        };
        self.metrics.record_gc(
            "lru",
            status,
            begin.elapsed().as_secs_f64() * 1000.0,
            removed,
        );
        result
    }

    fn delete_keys(&self, keys: Vec<AccessKey>) -> anyhow::Result<GcDeleteResult> {
        if keys.is_empty() {
            return Ok(GcDeleteResult {
                removed: 0,
                checksums: Vec::new(),
            });
        }
        let (tx, rx) = mpsc::sync_channel(1);
        self.writer_channel
            .submit_medium(WriteRequest::DeleteKeys { keys, reply: tx });
        rx.recv()
            .map_err(|_| anyhow::anyhow!("Writer thread dropped"))?
    }

    fn update_access<'env>(
        access_db: &Database<CheckSumOnDisk, AccessTime>,
        access_index: &Database<AccessKey, Bytes>,
        wtxn: &mut RwTxn<'env>,
        cs: &CheckSumOnDisk,
        now: u64,
    ) -> anyhow::Result<()> {
        if let Some(old) = access_db.get(wtxn, cs)? {
            if now <= old.secs || now.saturating_sub(old.secs) < ACCESS_UPDATE_MIN_INTERVAL_SECS {
                return Ok(());
            }
            let old_key = AccessKey::new(old.secs, *cs);
            access_index.delete(wtxn, &old_key)?;
        }
        let access_time = AccessTime { secs: now };
        access_db.put(wtxn, cs, &access_time)?;
        let new_key = AccessKey::new(now, *cs);
        access_index.put(wtxn, &new_key, &[])?;
        Ok(())
    }

    pub fn touch_chunk(&self, cs: &CheckSum) -> anyhow::Result<()> {
        let cs_on_disk = CheckSumOnDisk::from(*cs);
        self.enqueue_access_update(cs_on_disk);
        self.metrics.record_touch("ok");
        Ok(())
    }

    fn enqueue_access_update(&self, cs: CheckSumOnDisk) {
        self.writer_channel.enqueue_access(cs);
    }

    pub fn get_stats(&self) -> anyhow::Result<Value> {
        let info = self.env.info();
        let map_size = info.map_size as u64;
        let used_size = self.env.non_free_pages_size()?;
        let free_size = map_size.saturating_sub(used_size);
        let usage_ratio = if map_size > 0 {
            (used_size as f64 / map_size as f64) * 100.0
        } else {
            0.0
        };

        let rtxn = self.env.read_txn()?;
        let data_stat = self.data_db.stat(&rtxn)?;
        let total_chunks = data_stat.entries as u64;

        let mut oldest_access: Option<u64> = None;
        let mut newest_access: Option<u64> = None;

        if let Some(first) = self.access_index.first(&rtxn)? {
            oldest_access = Some(first.0.last_access);
        }
        if let Some(last) = self.access_index.last(&rtxn)? {
            newest_access = Some(last.0.last_access);
        }

        let num_readers = info.number_of_readers;
        let max_readers = info.maximum_number_of_readers;
        let stale_readers = self.env.clear_stale_readers()?;

        Ok(json!({
            "storage": {
                "total_size_bytes": map_size,
                "used_size_bytes": used_size,
                "free_size_bytes": free_size,
                "usage_percent": format!("{:.2}", usage_ratio),
            },
            "chunks": {
                "total_count": total_chunks,
            },
            "access_time": {
                "oldest_epoch_secs": oldest_access,
                "newest_epoch_secs": newest_access,
            },
            "readers": {
                "current": num_readers,
                "max": max_readers,
                "stale_cleared": stale_readers,
            },
        }))
    }

    fn spawn_writer_thread(
        env: Env,
        data_db: ChunkDataDb,
        access_db: ChunkAccessDb,
        access_index: ChunkAccessIndexDb,
        index_ctl: Option<Arc<dyn ChunkIndexControl>>,
        channel: Arc<WriterChannel>,
    ) -> thread::JoinHandle<()> {
        thread::spawn(move || {
            let writer =
                WriterThread::new(env, data_db, access_db, access_index, index_ctl, channel);
            writer.run();
        })
    }
}

impl Drop for ChunkDB {
    fn drop(&mut self) {
        {
            let mut q = self.writer_channel.inner.lock().unwrap();
            q.stop = true;
        }
        self.writer_channel.cond.notify_one();
        if let Some(handle) = self.writer_handle.take() {
            let _ = handle.join();
        }
    }
}

#[cfg(all(test, target_os = "linux"))]
mod tests;
