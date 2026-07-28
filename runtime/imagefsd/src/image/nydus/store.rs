use crate::backend::chunkdb::{CheckSum, ChunkDB};
use crate::image::nydus::decoded_cache::DecodedChunkCache;
use crate::rate_limited_log;
use crate::utils::RateLimitedLog;
use std::collections::BTreeSet;
use std::sync::atomic::AtomicU64;
use std::sync::atomic::Ordering;
use std::sync::mpsc::{self, Receiver, SyncSender};
use std::sync::{Arc, Mutex};
use std::thread;
use tracing::warn;

const CHUNK_STORE_REQ_BUF_SIZE: usize = 4096;
const CHUNK_STORE_BATCH_SIZE: usize = 64;

struct ChunkStoreRequest {
    checksum: CheckSum,
    data: Arc<[u8]>,
}

struct ChunkStoreWorker {
    chunk_db: std::sync::Arc<ChunkDB>,
    rx: Receiver<ChunkStoreRequest>,
    pending: Arc<Mutex<BTreeSet<CheckSum>>>,
    decoded_cache: Arc<DecodedChunkCache>,
}

impl ChunkStoreWorker {
    fn new(
        chunk_db: Arc<ChunkDB>,
        rx: Receiver<ChunkStoreRequest>,
        pending: Arc<Mutex<BTreeSet<CheckSum>>>,
        decoded_cache: Arc<DecodedChunkCache>,
    ) -> Self {
        Self {
            chunk_db,
            rx,
            pending,
            decoded_cache,
        }
    }

    fn run(self) {
        let mut batch = Vec::with_capacity(CHUNK_STORE_BATCH_SIZE);

        while let Ok(req) = self.rx.recv() {
            batch.push(req);

            while batch.len() < CHUNK_STORE_BATCH_SIZE {
                match self.rx.try_recv() {
                    Ok(req) => batch.push(req),
                    Err(mpsc::TryRecvError::Empty) => break,
                    Err(mpsc::TryRecvError::Disconnected) => break,
                }
            }

            self.store_batch(&mut batch);
        }
    }

    fn store_batch(&self, batch: &mut Vec<ChunkStoreRequest>) {
        let checksums = batch.iter().map(|req| req.checksum).collect::<Vec<_>>();
        let chunks = batch
            .drain(..)
            .map(|req| (req.checksum, req.data.as_ref().to_vec()))
            .collect();

        if let Err(e) = self.chunk_db.add_chunks(chunks) {
            warn!(err = debug(e), "Failed to store chunk batch into chunkdb.");
        } else {
            for checksum in &checksums {
                self.decoded_cache.invalidate(checksum);
            }
        }
        self.complete(&checksums);
    }

    fn complete(&self, checksums: &[CheckSum]) {
        let mut pending = self.pending.lock().unwrap();
        for checksum in checksums {
            pending.remove(checksum);
        }
    }
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub(super) struct ChunkStoreQueue {
    tx: Mutex<Option<SyncSender<ChunkStoreRequest>>>,
    worker: Mutex<Option<thread::JoinHandle<()>>>,
    dropped_count: AtomicU64,
    warn_limiter: RateLimitedLog,
    pending: Arc<Mutex<BTreeSet<CheckSum>>>,
}

impl ChunkStoreQueue {
    pub(super) fn new(chunk_db: Arc<ChunkDB>, decoded_cache: Arc<DecodedChunkCache>) -> Self {
        let (tx, rx) = mpsc::sync_channel(CHUNK_STORE_REQ_BUF_SIZE);
        let pending = Arc::new(Mutex::new(BTreeSet::new()));
        let worker = ChunkStoreWorker::new(chunk_db, rx, Arc::clone(&pending), decoded_cache);
        let handle = thread::spawn(move || worker.run());
        Self {
            tx: Mutex::new(Some(tx)),
            worker: Mutex::new(Some(handle)),
            dropped_count: AtomicU64::new(0),
            warn_limiter: RateLimitedLog::new(5),
            pending,
        }
    }

    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    pub(super) fn enqueue(&self, checksum: CheckSum, data: Arc<[u8]>) {
        if !self.pending.lock().unwrap().insert(checksum) {
            return;
        }
        let Some(tx) = self.tx.lock().unwrap().as_ref().cloned() else {
            self.pending.lock().unwrap().remove(&checksum);
            return;
        };
        let req = ChunkStoreRequest { checksum, data };
        if let Err(_e) = tx.try_send(req) {
            self.pending.lock().unwrap().remove(&checksum);
            let dropped = self.dropped_count.fetch_add(1, Ordering::Relaxed) + 1;
            rate_limited_log!(self.warn_limiter, {
                warn!(
                    dropped_count = dropped,
                    queue_size = CHUNK_STORE_REQ_BUF_SIZE,
                    "Chunk store queue full, dropping requests."
                );
                self.dropped_count.store(0, Ordering::Relaxed);
            });
        }
    }
}

impl Drop for ChunkStoreQueue {
    fn drop(&mut self) {
        let _ = self.tx.lock().unwrap().take();
        if let Ok(mut guard) = self.worker.lock() {
            if let Some(handle) = guard.take() {
                let _ = handle.join();
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::ChunkStoreQueue;
    use crate::backend::chunkdb::{CheckSum, CheckSumMethod, ChunkDB};
    use crate::image::nydus::decoded_cache::DecodedChunkCache;
    use std::sync::Arc;
    use std::thread;
    use std::time::{Duration, Instant};

    fn checksum(value: u8) -> CheckSum {
        CheckSum::from_data(&[value], CheckSumMethod::Blake3)
    }

    #[test]
    fn successful_store_releases_decoded_chunk() {
        let dir = tempfile::tempdir().unwrap();
        let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
        let cache = Arc::new(DecodedChunkCache::new(8));
        let key = checksum(1);
        let data = cache.get_or_load(key, || Ok(vec![1; 4])).unwrap().bytes;
        let queue = ChunkStoreQueue::new(Arc::clone(&db), Arc::clone(&cache));

        queue.enqueue(key, data);
        let deadline = Instant::now() + Duration::from_secs(2);
        while queue.pending.lock().unwrap().contains(&key) {
            assert!(Instant::now() < deadline, "chunk store worker timed out");
            thread::sleep(Duration::from_millis(5));
        }
        assert!(db.has_chunk(&key).unwrap());

        let mut reloaded = false;
        cache
            .get_or_load(key, || {
                reloaded = true;
                Ok(vec![1; 4])
            })
            .unwrap();
        assert!(reloaded, "persisted chunk remained in the decoded cache");
    }

    #[test]
    fn rejected_store_releases_pending_claim() {
        let dir = tempfile::tempdir().unwrap();
        let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
        let cache = Arc::new(DecodedChunkCache::new(8));
        let queue = ChunkStoreQueue::new(db, cache);
        queue.tx.lock().unwrap().take();

        let key = checksum(2);
        queue.enqueue(key, Arc::from(vec![2; 4]));
        assert!(!queue.pending.lock().unwrap().contains(&key));
    }
}
