use crate::backend::chunkdb::{CheckSum, CheckSumMethod, ChunkDB};
use crate::backend::indexdb::{DedupInfo, DedupRange, IndexDB};
use crate::backend::peer::LocalChunkClient;
use crate::backend::{BackendEx, CHUNK_SIZE};
use crate::utils::align_up;
use std::io;
use std::io::ErrorKind;
use std::sync::atomic::AtomicU64;
use std::sync::atomic::Ordering::{Relaxed, SeqCst};
use std::sync::Arc;
use tracing::{debug, info, warn};

use super::metrics::DedupMetrics;

#[derive(Debug)]
pub(crate) struct DedupCore {
    pub(crate) index_db: Arc<IndexDB>,
    pub(crate) chunk_db: Arc<ChunkDB>,
    pub(crate) b: Arc<dyn BackendEx>,
    pub(crate) id: [u8; 32],
    pub(crate) dedup_chunks: Vec<AtomicU64>,
    pub(crate) local_chunk_client: Option<Arc<LocalChunkClient>>,
    pub(crate) metrics: DedupMetrics,
}

impl DedupCore {
    pub(crate) fn new(
        b: Arc<dyn BackendEx>,
        chunk_db: Arc<ChunkDB>,
        index_db: Arc<IndexDB>,
        data_id: &str,
        local_chunk_client: Option<Arc<LocalChunkClient>>,
        metrics: DedupMetrics,
    ) -> Self {
        let id_hash = blake3::hash(data_id.as_bytes());
        let id = *id_hash.as_bytes();
        let nr_chunks = align_up(b.size() as usize, CHUNK_SIZE) / CHUNK_SIZE;
        let dedup_chunks = (0..align_up(nr_chunks, 64) / 64)
            .map(|_| AtomicU64::new(0))
            .collect();

        Self {
            index_db,
            chunk_db,
            b,
            id,
            dedup_chunks,
            local_chunk_client,
            metrics,
        }
    }

    pub(crate) fn chunk_is_dedup(&self, idx: usize) -> bool {
        let bit_at = idx / 64;
        let bit_off = idx % 64;
        let value = self.dedup_chunks[bit_at].load(Relaxed);
        value & (1_u64 << bit_off) != 0
    }

    pub(crate) fn mark_chunk_dedup(&self, idx: usize) {
        let bit_at = idx / 64;
        let bit_off = idx % 64;
        loop {
            let old = self.dedup_chunks[bit_at].load(Relaxed);
            let new = old | (1_u64 << bit_off);
            if old == new {
                break;
            }
            if self.dedup_chunks[bit_at]
                .compare_exchange(old, new, SeqCst, SeqCst)
                .is_ok()
            {
                break;
            }
        }
    }

    pub(crate) fn has_data(&self, cs: &CheckSum) -> anyhow::Result<bool> {
        self.chunk_db.has_chunk(cs)
    }

    pub(crate) fn add_data(&self, cs: &CheckSum, data: &[u8]) -> anyhow::Result<()> {
        let len = data.len();
        if let Err(err) = self.chunk_db.add_chunk(cs, data.to_vec()) {
            self.metrics.record_store("error", 0);
            return Err(err);
        }
        self.metrics.record_store("ok", len);
        Ok(())
    }

    pub(crate) fn add_local_dedup_info(
        &self,
        range: &DedupRange,
        cs: &CheckSum,
    ) -> anyhow::Result<()> {
        let mut wtxn = crate::backend::slow_write_txn(
            &self.index_db.env,
            "DedupReader::add_local_dedup_info",
        )?;
        let info = DedupInfo::new(range.start, range.size() as u32, *cs);
        self.index_db.storage.put(&mut wtxn, range, &info)?;
        wtxn.commit()?;
        Ok(())
    }

    pub(crate) fn dedup_check_range(&self, range: &DedupRange) -> anyhow::Result<bool> {
        let infos = self.dedup_range(range.start, range.size())?;
        if infos.len() != 1 {
            return Err(io::Error::new(ErrorKind::InvalidInput, "Invalid range").into());
        }
        Ok(infos[0].0.cs.is_valid())
    }

    pub(crate) fn dedup_range(
        &self,
        mut off: u64,
        mut len: usize,
    ) -> anyhow::Result<Vec<(DedupInfo, u64)>> {
        if len == 0 {
            return Ok(vec![]);
        }
        let rtxn = self.index_db.env.read_txn()?;
        let range = DedupRange::new_with_id(self.id, off, 1)..;
        let iter = self.index_db.storage.range(&rtxn, &range)?;
        let mut infos = vec![];
        for it in iter {
            let (k, v) = it?;
            if k.id != self.id {
                break;
            }
            if v.off > off {
                let invalid_len = (v.off - off).min(len as u64);
                infos.push((
                    DedupInfo::new(off, invalid_len as u32, CheckSum::empty()),
                    invalid_len,
                ));
                off += invalid_len;
                len -= invalid_len as usize;
                if len == 0 {
                    break;
                }
            } else if v.off <= off {
                if v.end() <= off {
                    continue;
                }
                let valid_len = (v.end() - off).min(len as u64);
                infos.push((*v, valid_len));
                off += valid_len;
                len -= valid_len as usize;
            }
            if len == 0 || v.off >= off + (len as u64) {
                break;
            }
        }
        if len != 0 {
            infos.push((
                DedupInfo::new(off, len as u32, CheckSum::empty()),
                len as u64,
            ));
        }
        Ok(infos)
    }

    pub(crate) fn dedup(
        &self,
        off: u64,
        len: u32,
        cs: Option<CheckSum>,
        mut method: CheckSumMethod,
    ) -> anyhow::Result<()> {
        if len == 0 {
            return Ok(());
        }

        let backend_size = self.b.size();
        if off >= backend_size {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                format!(
                    "Offset {} is out of range for backend size {}",
                    off, backend_size
                ),
            )
            .into());
        }

        if !off.is_multiple_of(CHUNK_SIZE as u64) {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                format!(
                    "Offset {} is not aligned to CHUNK_SIZE ({})",
                    off, CHUNK_SIZE
                ),
            )
            .into());
        }
        let end = off.checked_add(len as u64).ok_or_else(|| {
            io::Error::new(
                ErrorKind::InvalidInput,
                format!("Range overflow: off={}, len={}", off, len),
            )
        })?;
        let is_last_chunk = end >= backend_size;
        if !len.is_multiple_of(CHUNK_SIZE as u32) && !is_last_chunk {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                format!(
                    "Length {} is not aligned to CHUNK_SIZE ({})",
                    len, CHUNK_SIZE
                ),
            )
            .into());
        }

        let range = DedupRange::new_with_id(self.id, off, len);
        if self.dedup_check_range(&range)? {
            debug!(off = off, len = len, "Data is duplicated.");
            return Ok(());
        }
        info!(off = off, len = len, "Dedup backend.");
        if let Some(checksum) = cs.as_ref() {
            if self.has_data(checksum)? {
                if let Err(e) = self.chunk_db.touch_chunk(checksum) {
                    warn!(err = debug(e), "Failed to update chunk access time.");
                }
                self.add_local_dedup_info(&range, checksum)?;
                return Ok(());
            }
            method = checksum.method;
        }
        let mut buf = vec![0_u8; len as usize];
        let n = self.b.fetch(off as usize, &mut buf)?;
        if n != len as usize {
            if off + (n as u64) != backend_size {
                return Err(io::Error::new(
                    ErrorKind::UnexpectedEof,
                    format!(
                        "Short read: expected {} bytes, got {} bytes at offset {}, backend size {}",
                        len, n, off, backend_size
                    ),
                )
                .into());
            }
            buf.truncate(n);
            debug!(
                off = off,
                expected = len,
                actual = n,
                "Partial chunk at EOF, deduplicating {} bytes",
                n
            );
        }
        let data_cs = CheckSum::from_data(&buf, method);
        self.add_data(&data_cs, &buf)?;
        let actual_range = DedupRange::new_with_id(self.id, off, n as u32);
        self.add_local_dedup_info(&actual_range, &data_cs)?;
        Ok(())
    }

    pub(crate) fn gc_chunk(&self, chunk_id: usize) -> anyhow::Result<()> {
        let start = chunk_id * CHUNK_SIZE;
        if start as u64 > self.b.size() {
            return Ok(());
        }
        let end = ((chunk_id + 1) * CHUNK_SIZE).min(self.b.size() as usize);
        let infos = self.dedup_range(start as u64, end - start)?;
        for (info, _) in &infos {
            if !info.cs.is_valid() {
                return Ok(());
            }
        }
        self.b.invalidate_chunk(chunk_id)?;
        Ok(())
    }
}
