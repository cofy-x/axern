use crate::backend::chunkdb::CheckSum;
use crate::backend::indexdb::DedupInfo;
use crate::backend::CHUNK_SIZE;
use std::sync::mpsc::Receiver;
use std::sync::Arc;
use tracing::{debug, error, info, warn};

use super::{DedupCore, DedupRequest};

#[derive(Debug)]
pub(crate) struct SyncWorker {
    core: Arc<DedupCore>,
    rx: Receiver<DedupInfo>,
}

impl SyncWorker {
    pub(crate) fn new(core: Arc<DedupCore>, rx: Receiver<DedupInfo>) -> Self {
        Self { core, rx }
    }

    pub(crate) fn run(self) {
        while let Ok(info) = self.rx.recv() {
            self.process(info);
        }
    }

    pub(crate) fn process(&self, info: DedupInfo) {
        if !info.cs.is_valid() || info.len == 0 {
            return;
        }
        let cs = CheckSum::from(info.cs);
        let len = info.len as usize;
        let mut buf = vec![0_u8; len];
        let n = match self.core.b.fetch(info.off as usize, &mut buf) {
            Ok(n) => n,
            Err(e) => {
                warn!(err = debug(e), "Failed to fetch data for sync.");
                return;
            }
        };
        if n != len {
            warn!(
                expected = len,
                actual = n,
                "Short read when syncing dedup chunk."
            );
            return;
        }
        let data_cs = CheckSum::from_data(&buf, cs.method);
        if data_cs != cs {
            warn!("Checksum mismatch when syncing dedup chunk.");
            return;
        }
        let buf_len = buf.len();
        if let Err(e) = self.core.chunk_db.add_chunk(&cs, buf) {
            self.core.metrics.record_store("error", 0);
            warn!(err = debug(e), "Failed to add synced chunk to chunkdb.");
            return;
        }
        self.core.metrics.record_store("ok", buf_len);

        for chunk_id in
            (info.off as usize) / CHUNK_SIZE..=(info.off as usize + len - 1) / CHUNK_SIZE
        {
            if let Err(e) = self.core.gc_chunk(chunk_id) {
                debug!(
                    chunk_id = chunk_id,
                    err = debug(e),
                    "Failed to gc chunk after sync."
                );
            }
        }
    }
}

#[derive(Debug)]
pub(crate) struct DedupWorker {
    core: Arc<DedupCore>,
    requests: Receiver<DedupRequest>,
}

impl DedupWorker {
    pub(crate) fn new(core: Arc<DedupCore>, requests: Receiver<DedupRequest>) -> Self {
        Self { core, requests }
    }

    pub(crate) fn run(self) {
        loop {
            match self.requests.recv() {
                Ok(req) => {
                    if let Err(e) = self.core.dedup(req.off, req.len, req.check_sum, req.method) {
                        error!(req = debug(req), err = debug(e), "Failed to do dedup.");
                    } else {
                        if req.size() == 0 {
                            continue;
                        }
                        let start_chunk = (req.offset() as usize) / CHUNK_SIZE;
                        let end_chunk = ((req.offset() as usize) + req.size() - 1) / CHUNK_SIZE;
                        info!(
                            start = start_chunk,
                            end = end_chunk,
                            "Start trying to gc chunks"
                        );
                        for chunk_id in start_chunk..=end_chunk {
                            if let Err(e) = self.core.gc_chunk(chunk_id) {
                                error!(
                                    chunk_id = chunk_id,
                                    error = debug(e),
                                    "Failed to gc chunk."
                                );
                            }
                        }
                    }
                }
                Err(e) => {
                    error!(err = debug(e), "Failed to recv dedup requests.");
                    break;
                }
            }
        }
    }
}
