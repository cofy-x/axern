#[cfg(target_os = "linux")]
use crate::backend::cache::Cache;
use crate::backend::chunkdb::ChunkDB;
#[cfg(target_os = "linux")]
use crate::backend::chunkdb::{CheckSum, CheckSumMethod};
#[cfg(target_os = "linux")]
use crate::backend::dedup::DedupReader;
use crate::backend::general::GeneralBackend;
use crate::backend::indexdb::IndexDB;
use crate::backend::peer::LocalChunkClient;
use crate::backend::Backend;
#[cfg(target_os = "linux")]
use crate::backend::BackendEx;
#[cfg(target_os = "linux")]
use crate::backend::CHUNK_SIZE;
#[cfg(target_os = "linux")]
use crate::utils::new_std_io_error;
#[cfg(target_os = "linux")]
use fuse_backend_rs::api::filesystem::ZeroCopyWriter;
#[cfg(target_os = "linux")]
use nydus_storage::device::{BlobChunkInfo, BlobInfo};
#[cfg(target_os = "linux")]
use nydus_utils::compress;
#[cfg(target_os = "linux")]
use nydus_utils::digest::Algorithm as DigestAlgorithm;
use std::collections::HashMap;
use std::io;
#[cfg(target_os = "linux")]
use std::io::ErrorKind;
#[cfg(target_os = "linux")]
use std::sync::atomic::{AtomicBool, Ordering};
#[cfg(target_os = "linux")]
use std::sync::mpsc::{self, Receiver, SyncSender, TrySendError};
use std::sync::{Arc, Mutex};
#[cfg(target_os = "linux")]
use std::thread::{self, JoinHandle};
#[cfg(target_os = "linux")]
use tracing::error;
#[cfg(target_os = "linux")]
use tracing::warn;

#[cfg(target_os = "linux")]
use super::decoded_cache::DecodedChunk;
use super::decoded_cache::DecodedChunkCache;
use super::NydusCacheConfig;

#[cfg(target_os = "linux")]
struct ReadaheadTask {
    cache: Arc<Cache<crate::backend::general::BackendReader>>,
    start: usize,
    len: usize,
}

#[cfg(target_os = "linux")]
struct ReadaheadPool {
    sender: Option<SyncSender<ReadaheadTask>>,
    cancelled: Arc<AtomicBool>,
    workers: Vec<JoinHandle<()>>,
}

#[cfg(target_os = "linux")]
impl ReadaheadPool {
    fn new(worker_count: usize) -> io::Result<Option<Self>> {
        if worker_count == 0 {
            return Ok(None);
        }
        let queue_capacity = worker_count.saturating_mul(2).max(1);
        let (sender, receiver) = mpsc::sync_channel::<ReadaheadTask>(queue_capacity);
        let receiver = Arc::new(Mutex::new(receiver));
        let cancelled = Arc::new(AtomicBool::new(false));
        let workers = (0..worker_count)
            .map(|idx| Self::spawn_worker(idx, Arc::clone(&receiver), Arc::clone(&cancelled)))
            .collect::<io::Result<Vec<_>>>()?;
        Ok(Some(Self {
            sender: Some(sender),
            cancelled,
            workers,
        }))
    }

    fn spawn_worker(
        idx: usize,
        receiver: Arc<Mutex<Receiver<ReadaheadTask>>>,
        cancelled: Arc<AtomicBool>,
    ) -> io::Result<JoinHandle<()>> {
        thread::Builder::new()
            .name(format!("nydus-readahead-{idx}"))
            .spawn(move || loop {
                let task = receiver.lock().unwrap().recv();
                let Ok(task) = task else {
                    break;
                };
                if cancelled.load(Ordering::Acquire) {
                    break;
                }
                match task
                    .cache
                    .readahead(task.start, task.len, cancelled.as_ref())
                {
                    Ok(outcome) => tracing::debug!(
                        start = task.start,
                        len = task.len,
                        bytes = outcome.bytes,
                        cached_chunks = outcome.cached_chunks,
                        skipped_chunks = outcome.skipped_chunks,
                        "Nydus bounded readahead completed."
                    ),
                    Err(err) => {
                        warn!(err = debug(err), "Nydus bounded readahead failed.");
                    }
                }
            })
    }

    fn sender(&self) -> Option<SyncSender<ReadaheadTask>> {
        self.sender.clone()
    }
}

#[cfg(target_os = "linux")]
impl Drop for ReadaheadPool {
    fn drop(&mut self) {
        self.cancelled.store(true, Ordering::Release);
        self.sender.take();
        for worker in self.workers.drain(..) {
            if worker.join().is_err() {
                warn!("Nydus bounded readahead worker panicked.");
            }
        }
    }
}

#[cfg(target_os = "linux")]
#[derive(Debug)]
struct ReadaheadBackend {
    cache: Arc<Cache<crate::backend::general::BackendReader>>,
    sender: SyncSender<ReadaheadTask>,
    window_bytes: usize,
}

#[cfg(target_os = "linux")]
impl ReadaheadBackend {
    fn schedule_after(&self, end: usize) {
        let size = self.cache.size() as usize;
        if end >= size || self.window_bytes == 0 {
            return;
        }
        let start = end.saturating_add(CHUNK_SIZE - 1) / CHUNK_SIZE * CHUNK_SIZE;
        if start >= size {
            return;
        }
        match self.sender.try_send(ReadaheadTask {
            cache: Arc::clone(&self.cache),
            start,
            len: start.saturating_add(self.window_bytes).min(size) - start,
        }) {
            Ok(()) => {}
            Err(TrySendError::Full(_)) => {}
            Err(TrySendError::Disconnected(_)) => {
                warn!("Nydus readahead queue disconnected.");
            }
        }
    }
}

#[cfg(target_os = "linux")]
impl Backend for ReadaheadBackend {
    fn size(&self) -> u64 {
        self.cache.size()
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> io::Result<usize> {
        let read = self.cache.fetch(off, data)?;
        self.schedule_after(off.saturating_add(read));
        Ok(read)
    }
}

#[cfg(target_os = "linux")]
impl BackendEx for ReadaheadBackend {
    fn invalidate_chunk(&self, chunk_id: usize) -> io::Result<()> {
        self.cache.invalidate_chunk(chunk_id)
    }
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub(super) struct BlobIo {
    readers: Mutex<HashMap<String, Arc<dyn Backend>>>,
    backend: Arc<GeneralBackend>,
    dedup_db: Option<(Arc<ChunkDB>, Arc<IndexDB>)>,
    local_chunk_client: Option<Arc<LocalChunkClient>>,
    cache_dir: String,
    #[cfg(target_os = "linux")]
    readahead_pool: Option<ReadaheadPool>,
    #[cfg(target_os = "linux")]
    readahead_window_bytes: usize,
    decoded_chunks: Arc<DecodedChunkCache>,
    node_id: String,
}

impl BlobIo {
    pub(super) fn new(
        backend: Arc<GeneralBackend>,
        cache_dir: String,
        dedup_db: Option<(Arc<ChunkDB>, Arc<IndexDB>)>,
        local_chunk_client: Option<Arc<LocalChunkClient>>,
        cache_config: NydusCacheConfig,
    ) -> io::Result<Self> {
        #[cfg(not(target_os = "linux"))]
        let _ = (
            cache_config.readahead_workers,
            cache_config.readahead_window_bytes,
            cache_config.decoded_cache_bytes,
            cache_config.node_id.clone(),
        );
        Ok(Self {
            readers: Mutex::new(HashMap::new()),
            backend,
            dedup_db,
            local_chunk_client,
            cache_dir,
            #[cfg(target_os = "linux")]
            readahead_pool: ReadaheadPool::new(cache_config.readahead_workers)?,
            #[cfg(target_os = "linux")]
            readahead_window_bytes: cache_config.readahead_window_bytes,
            decoded_chunks: Arc::new(DecodedChunkCache::new(cache_config.decoded_cache_bytes)),
            node_id: cache_config.node_id,
        })
    }

    pub(super) fn decoded_chunk_cache(&self) -> Arc<DecodedChunkCache> {
        Arc::clone(&self.decoded_chunks)
    }

    #[cfg(target_os = "linux")]
    pub(super) fn get_blob_reader(&self, blob: &BlobInfo) -> io::Result<Arc<dyn Backend>> {
        let blob_id = blob.blob_id();
        let mut readers = self.readers.lock().unwrap();
        if let Some(reader) = readers.get(&blob_id) {
            return Ok(reader.clone());
        }

        let reader = self
            .backend
            .get_reader_with_size(&blob_id, blob.compressed_size())
            .inspect_err(|e| {
                error!(
                    err = debug(e),
                    blob_id, "Failed to open backend reader for Nydus blob."
                );
            })?;
        if self.cache_dir.is_empty() {
            let direct_reader = Arc::new(reader);
            readers.insert(blob_id, direct_reader.clone());
            Ok(direct_reader)
        } else {
            let cache_file = format!("{}/{}", self.cache_dir, blob_id);
            let reader_with_cache = Arc::new(
                Cache::new_with_node_id(reader, &cache_file, &self.node_id)
                    .map_err(new_std_io_error)
                    .inspect_err(|e| {
                        error!(
                            err = debug(e),
                            blob_id,
                            cache_file,
                            "Failed to create cached backend reader for Nydus blob."
                        );
                    })?,
            );
            let cached_reader: Arc<dyn BackendEx> =
                match self.readahead_pool.as_ref().and_then(ReadaheadPool::sender) {
                    Some(sender) if self.readahead_window_bytes > 0 => Arc::new(ReadaheadBackend {
                        cache: reader_with_cache.clone(),
                        sender,
                        window_bytes: self.readahead_window_bytes,
                    }),
                    _ => reader_with_cache.clone(),
                };
            let final_reader = if let Some((chunk_db, index_db)) = &self.dedup_db {
                let dedup = DedupReader::new(
                    cached_reader,
                    chunk_db.clone(),
                    index_db.clone(),
                    &blob_id,
                    self.local_chunk_client.clone(),
                )
                .map_err(new_std_io_error)
                .inspect_err(|e| {
                    error!(
                        err = debug(e),
                        blob_id, "Failed to create dedup backend reader for Nydus blob."
                    );
                })?;
                Arc::new(dedup) as Arc<dyn Backend>
            } else {
                cached_reader as Arc<dyn Backend>
            };
            readers.insert(blob_id, final_reader.clone());
            Ok(final_reader)
        }
    }

    #[cfg(target_os = "linux")]
    pub(super) fn checksum_from_chunk(
        &self,
        chunk: &dyn BlobChunkInfo,
        blob: &BlobInfo,
    ) -> io::Result<CheckSum> {
        let method = match blob.digester() {
            DigestAlgorithm::Blake3 => CheckSumMethod::Blake3,
            DigestAlgorithm::Sha256 => CheckSumMethod::Sha256,
        };
        CheckSum::new(chunk.chunk_id().as_ref(), method)
    }

    #[cfg(target_os = "linux")]
    fn read_blob_range(
        &self,
        reader: &dyn Backend,
        offset: u64,
        size: usize,
    ) -> io::Result<Vec<u8>> {
        let mut buf = vec![0_u8; size];
        let mut read = 0;
        while read < size {
            let n = reader.fetch((offset as usize) + read, &mut buf[read..])?;
            if n == 0 {
                return Err(io::Error::new(ErrorKind::UnexpectedEof, "short read"));
            }
            read += n;
        }
        Ok(buf)
    }

    #[cfg(target_os = "linux")]
    pub(super) fn read_chunk_data(
        &self,
        checksum: CheckSum,
        chunk: &dyn BlobChunkInfo,
        blob: &BlobInfo,
    ) -> io::Result<DecodedChunk> {
        self.decoded_chunks
            .get_or_load(checksum, || self.load_chunk_data(chunk, blob))
    }

    #[cfg(target_os = "linux")]
    fn load_chunk_data(&self, chunk: &dyn BlobChunkInfo, blob: &BlobInfo) -> io::Result<Vec<u8>> {
        if chunk.is_encrypted() {
            return Err(io::Error::new(
                ErrorKind::Unsupported,
                "encrypted chunk is not supported",
            ));
        }
        if chunk.is_batch() {
            return Err(io::Error::new(
                ErrorKind::Unsupported,
                "batch chunk is not supported",
            ));
        }
        let compressed_size = chunk.compressed_size() as usize;
        let uncompressed_size = chunk.uncompressed_size() as usize;
        if uncompressed_size == 0 {
            return Ok(Vec::new());
        }
        if compressed_size == 0 {
            return Ok(vec![0_u8; uncompressed_size]);
        }

        let reader = self.get_blob_reader(blob)?;
        let mut compressed =
            self.read_blob_range(reader.as_ref(), chunk.compressed_offset(), compressed_size)?;

        if !chunk.is_compressed() {
            if compressed.len() < uncompressed_size {
                return Err(io::Error::new(
                    ErrorKind::InvalidData,
                    "compressed data is shorter than expected",
                ));
            }
            compressed.truncate(uncompressed_size);
            return Ok(compressed);
        }

        let mut data = vec![0_u8; uncompressed_size];
        let sz = compress::decompress(&compressed, &mut data, blob.compressor()).map_err(|e| {
            io::Error::new(ErrorKind::InvalidData, format!("decompress failed: {}", e))
        })?;
        if sz != uncompressed_size {
            return Err(io::Error::new(
                ErrorKind::InvalidData,
                format!("decompressed chunk size mismatch: expected {uncompressed_size}, got {sz}"),
            ));
        }
        Ok(data)
    }

    #[cfg(target_os = "linux")]
    pub(super) fn read_from_chunkdb(
        &self,
        cs: &CheckSum,
        start: usize,
        len: usize,
        w: &mut dyn ZeroCopyWriter,
    ) -> io::Result<Option<usize>> {
        if let Some((chunk_db, _)) = &self.dedup_db {
            match chunk_db.with_chunk_range(cs, start, len, |slice| w.write(slice)) {
                Ok(Some(n)) => Ok(Some(n)),
                Ok(None) => Ok(None),
                Err(e) => {
                    warn!(err = debug(e), "Failed to read chunk from chunkdb.");
                    Ok(None)
                }
            }
        } else {
            Ok(None)
        }
    }
}
