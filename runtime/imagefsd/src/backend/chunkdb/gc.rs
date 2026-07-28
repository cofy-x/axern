use std::io::{self, ErrorKind};
use std::path::Path;
use std::sync::Arc;
use std::time::Duration;
use tracing::{debug, info};

use super::{
    CheckSum, ChunkDB, ChunkIndexControl, DEFAULT_GC_EXPIRE_SECS, GC_BATCH, GC_HIGH_WATERMARK,
    GC_LOW_WATERMARK,
};

pub struct GcWorker {
    chunk_db: ChunkDB,
    local_chunk_client: Option<Arc<dyn ChunkIndexControl>>,
    expire_after: Duration,
    high_watermark: f64,
    low_watermark: f64,
}

impl GcWorker {
    pub fn new_with_local_client<P: AsRef<Path>>(
        chunk_db_dir: P,
        local_chunk_client: Option<Arc<dyn ChunkIndexControl>>,
    ) -> anyhow::Result<Self> {
        Self::new_with_opts_and_client(
            chunk_db_dir,
            Duration::from_secs(DEFAULT_GC_EXPIRE_SECS),
            GC_HIGH_WATERMARK,
            GC_LOW_WATERMARK,
            local_chunk_client,
        )
    }

    pub fn new_with_opts_and_client<P: AsRef<Path>>(
        chunk_db_dir: P,
        expire_after: Duration,
        high_watermark: f64,
        low_watermark: f64,
        local_chunk_client: Option<Arc<dyn ChunkIndexControl>>,
    ) -> anyhow::Result<Self> {
        if !(0.0..=1.0).contains(&high_watermark)
            || !(0.0..=1.0).contains(&low_watermark)
            || low_watermark > high_watermark
        {
            return Err(io::Error::new(ErrorKind::InvalidInput, "Invalid GC watermark").into());
        }
        let chunk_db = ChunkDB::new(chunk_db_dir)?;
        Ok(Self {
            chunk_db,
            local_chunk_client,
            expire_after,
            high_watermark,
            low_watermark,
        })
    }

    fn usage_ratio(&self) -> anyhow::Result<f64> {
        let info = self.chunk_db.env.info();
        let map_size = info.map_size as f64;
        if map_size <= 0.0 {
            return Ok(0.0);
        }
        let used = self.chunk_db.env.non_free_pages_size()? as f64;
        Ok((used / map_size).min(1.0))
    }

    pub fn run(&self, dry_run: bool) -> anyhow::Result<()> {
        if dry_run {
            info!("GcWorker dry run enabled; skipping deletions.");
            return Ok(());
        }
        let expired = self.chunk_db.gc_expired(self.expire_after)?;
        self.unregister_chunks(&expired.checksums);
        info!(expired = expired.removed, "GC expired chunks completed.");
        loop {
            let usage = self.usage_ratio()?;
            if usage <= self.high_watermark {
                break;
            }
            let removed = self.chunk_db.gc_lru(GC_BATCH)?;
            self.unregister_chunks(&removed.checksums);
            info!(
                removed = removed.removed,
                usage = usage,
                "GC LRU batch completed."
            );
            if removed.removed == 0 {
                break;
            }
            let new_usage = self.usage_ratio()?;
            if new_usage <= self.low_watermark {
                break;
            }
        }
        Ok(())
    }

    fn unregister_chunks(&self, checksums: &[CheckSum]) {
        let Some(client) = &self.local_chunk_client else {
            return;
        };
        if !client.unregister_chunks(checksums) {
            debug!(
                count = checksums.len(),
                "Failed to unregister chunks with local chunk server during GC."
            );
        }
    }
}
