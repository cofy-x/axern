mod core;
mod metrics;
mod transfer;
mod workers;

use crate::backend::chunkdb::CheckSumMethod::Blake3;
use crate::backend::chunkdb::ChunkDB;
pub use crate::backend::chunkdb::{CheckSum, CheckSumMethod};
use crate::backend::indexdb::{DedupInfo, IndexDB};
use crate::backend::peer::LocalChunkClient;
use crate::backend::{Backend, BackendEx, CHUNK_SIZE};
#[cfg(target_os = "linux")]
use fuse_backend_rs::api::filesystem::ZeroCopyWriter;
use opentelemetry::global;
use std::io;
use std::sync::mpsc::{self, SyncSender};
use std::sync::Arc;
use std::thread;
use std::time::Instant;
use tracing::{error, warn};

use self::core::DedupCore;
use self::metrics::DedupMetrics;
#[cfg(target_os = "linux")]
use self::transfer::FuseWriteOp;
use self::transfer::{read_prefetched_chunk, DataTransfer, FetchOp};
use self::workers::{DedupWorker, SyncWorker};

const SYNC_CHUNK_REQ_BUF_SIZE: usize = 256;
const DEDUP_CHUNK_REQ_BUF_SIZE: usize = 256;

#[derive(Debug)]
pub struct DedupReader {
    core: Arc<DedupCore>,
    sync_tx: SyncSender<DedupInfo>,
    dedup_tx: SyncSender<DedupRequest>,
}

impl DedupReader {
    pub fn new(
        b: Arc<dyn BackendEx>,
        chunk_db: Arc<ChunkDB>,
        index_db: Arc<IndexDB>,
        data_id: &str,
        local_chunk_client: Option<Arc<LocalChunkClient>>,
    ) -> anyhow::Result<Self> {
        let meter = global::meter("imagefsd.dedup");
        Self::new_with_metrics(
            b,
            chunk_db,
            index_db,
            data_id,
            local_chunk_client,
            DedupMetrics::new(&meter),
        )
    }

    fn new_with_metrics(
        b: Arc<dyn BackendEx>,
        chunk_db: Arc<ChunkDB>,
        index_db: Arc<IndexDB>,
        data_id: &str,
        local_chunk_client: Option<Arc<LocalChunkClient>>,
        metrics: DedupMetrics,
    ) -> anyhow::Result<Self> {
        let (sync_tx, sync_rx) = mpsc::sync_channel(SYNC_CHUNK_REQ_BUF_SIZE);
        let (dedup_tx, dedup_rx) = mpsc::sync_channel(DEDUP_CHUNK_REQ_BUF_SIZE);

        let core = Arc::new(DedupCore::new(
            b,
            chunk_db,
            index_db,
            data_id,
            local_chunk_client,
            metrics,
        ));

        let sync_worker_core = Arc::clone(&core);
        let sync_worker_handle = SyncWorker::new(sync_worker_core, sync_rx);
        thread::spawn(move || sync_worker_handle.run());

        let dedup_worker_core = Arc::clone(&core);
        let dedup_worker_handle = DedupWorker::new(dedup_worker_core, dedup_rx);
        thread::spawn(move || dedup_worker_handle.run());

        Ok(Self {
            core,
            sync_tx,
            dedup_tx,
        })
    }

    #[cfg(all(test, target_os = "linux"))]
    fn dedup(
        &self,
        off: u64,
        len: u32,
        cs: Option<CheckSum>,
        method: CheckSumMethod,
    ) -> anyhow::Result<()> {
        self.core.dedup(off, len, cs, method)
    }

    #[cfg(all(test, target_os = "linux"))]
    fn gc_chunk(&self, chunk_id: usize) -> anyhow::Result<()> {
        self.core.gc_chunk(chunk_id)
    }

    fn enqueue_sync(&self, info: DedupInfo) {
        if let Err(e) = self.sync_tx.try_send(info) {
            warn!(err = debug(e), "Failed to enqueue dedup sync task.");
        }
    }

    fn try_dedup_chunk(&self, off: usize, len: usize, method: CheckSumMethod) {
        if len == 0 {
            return;
        }
        let chunk_start = off / CHUNK_SIZE;
        let chunk_end = (off + len - 1) / CHUNK_SIZE;

        for idx in chunk_start..=chunk_end {
            if self.core.chunk_is_dedup(idx) {
                continue;
            }
            let req = DedupRequest::new((idx * CHUNK_SIZE) as u64, CHUNK_SIZE as u32, method);
            if let Err(e) = self.dedup_tx.try_send(req) {
                warn!(err = debug(e), "Failed to send dedup request.")
            } else {
                self.core.mark_chunk_dedup(idx);
            }
        }
    }

    fn process_dedup_op<T: DataTransfer>(
        &self,
        off: usize,
        len: usize,
        op: &mut T,
    ) -> io::Result<usize> {
        let infos = match self.core.dedup_range(off as u64, len) {
            Ok(infos) => infos,
            Err(e) => {
                self.core.metrics.record_backend_fallback("range_error");
                error!(
                    err = debug(e),
                    "Failed to dedup_range, fallback to backend op."
                );
                return op.copy_from_backend(off, 0, len);
            }
        };
        if infos.is_empty() {
            return op.copy_from_backend(off, 0, len);
        }
        let mut pos = 0_usize;
        let mut whole_off = off as u64;
        for (info, seg_len) in infos {
            let seg_len_usize = seg_len as usize;
            let n = if !info.cs.is_valid() {
                self.core
                    .metrics
                    .record_backend_fallback("missing_metadata");
                op.copy_from_backend(whole_off as usize, pos, seg_len_usize)?
            } else {
                let cs = CheckSum::from(info.cs);
                let start = (whole_off - info.off) as usize;
                match self.read_dedup_chunk(&cs, start, seg_len_usize, op, pos) {
                    Ok(Some(n)) => {
                        self.core.metrics.record_chunk_hit();
                        n
                    }
                    Ok(None) => {
                        self.enqueue_sync(info);
                        self.core.metrics.record_backend_fallback("chunk_miss");
                        error!("Failed to get dedup data, fallback to backend op.");
                        op.copy_from_backend(whole_off as usize, pos, seg_len_usize)?
                    }
                    Err(e) => {
                        self.core.metrics.record_backend_fallback("chunk_error");
                        error!(
                            err = debug(e),
                            "Failed to get dedup data, fallback to backend op."
                        );
                        op.copy_from_backend(whole_off as usize, pos, seg_len_usize)?
                    }
                }
            };
            pos += n;
            whole_off += n as u64;
        }
        Ok(pos)
    }

    fn read_dedup_chunk<T: DataTransfer>(
        &self,
        cs: &CheckSum,
        start: usize,
        len: usize,
        op: &mut T,
        pos: usize,
    ) -> io::Result<Option<usize>> {
        read_prefetched_chunk(self, cs, start, len, op, pos)
    }
}

impl Backend for DedupReader {
    fn size(&self) -> u64 {
        self.core.b.size()
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> io::Result<usize> {
        let begin = Instant::now();
        if data.is_empty() {
            self.core.metrics.record_read("ok", 0.0);
            return Ok(0);
        }
        let size = self.core.b.size() as usize;
        if off >= size {
            self.core.metrics.record_read("ok", 0.0);
            return Ok(0);
        }
        let len = data.len().min(size - off);
        let mut op = FetchOp { dedup: self, data };
        let res = self.process_dedup_op(off, len, &mut op);
        if res.is_ok() {
            self.try_dedup_chunk(off, len, Blake3);
        }
        let status = if res.is_ok() { "ok" } else { "error" };
        self.core
            .metrics
            .record_read(status, begin.elapsed().as_secs_f64() * 1000.0);
        res
    }

    #[cfg(target_os = "linux")]
    fn write_to_fuse_writer(
        &self,
        off: usize,
        size: u32,
        w: &mut dyn ZeroCopyWriter,
    ) -> io::Result<usize> {
        let begin = Instant::now();
        if size == 0 {
            self.core.metrics.record_read("ok", 0.0);
            return Ok(0);
        }
        let total = self.core.b.size() as usize;
        if off >= total {
            self.core.metrics.record_read("ok", 0.0);
            return Ok(0);
        }
        let len = (size as usize).min(total - off);
        let mut op = FuseWriteOp {
            reader: self,
            writer: w,
        };
        let res = self.process_dedup_op(off, len, &mut op);
        if res.is_ok() {
            self.try_dedup_chunk(off, len, Blake3);
        }
        let status = if res.is_ok() { "ok" } else { "error" };
        self.core
            .metrics
            .record_read(status, begin.elapsed().as_secs_f64() * 1000.0);
        res
    }
}

#[derive(Debug, Eq, PartialEq)]
pub struct DedupRequest {
    off: u64,
    len: u32,
    check_sum: Option<CheckSum>,
    method: CheckSumMethod,
}

impl DedupRequest {
    pub fn new(off: u64, len: u32, method: CheckSumMethod) -> Self {
        Self {
            off,
            len,
            check_sum: None,
            method,
        }
    }

    pub fn offset(&self) -> u64 {
        self.off
    }

    pub fn size(&self) -> usize {
        self.len as usize
    }
}

#[cfg(all(test, target_os = "linux"))]
mod tests;
