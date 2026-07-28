use crate::backend::chunkdb::{CheckSum, ChunkDB};
use crate::backend::peer::index::ChunkIndex;
use crate::backend::peer::ShutdownHandle;
use rand::Rng;
use std::sync::Arc;
use std::time::Duration;
use tokio::task::block_in_place;
use tokio::time::interval;
use tracing::{info, warn};

pub(super) struct IndexMaintainer {
    chunk_db: Arc<ChunkDB>,
    chunk_index: Arc<dyn ChunkIndex>,
}

impl IndexMaintainer {
    pub(super) fn new(chunk_db: Arc<ChunkDB>, chunk_index: Arc<dyn ChunkIndex>) -> Self {
        Self {
            chunk_db,
            chunk_index,
        }
    }

    async fn read_chunk_batch(
        &self,
        cursor: Option<CheckSum>,
        batch_size: usize,
    ) -> anyhow::Result<Vec<CheckSum>> {
        let chunk_db = Arc::clone(&self.chunk_db);
        block_in_place(|| chunk_db.next_chunk_batch(cursor, batch_size))
    }

    pub(super) async fn sync(self: Arc<Self>) {
        let mut total = 0usize;
        let mut cursor = None;
        loop {
            let batch = match self
                .read_chunk_batch(cursor, super::super::INDEX_SYNC_BATCH_SIZE)
                .await
            {
                Ok(batch) => batch,
                Err(err) => {
                    warn!(
                        err = tracing::field::debug(err),
                        count = total,
                        "failed to register existing chunks in index"
                    );
                    return;
                }
            };
            if batch.is_empty() {
                info!(count = total, "registered existing chunks in index");
                return;
            }
            cursor = batch.last().copied();
            match self.chunk_index.sync_existing_chunks(&batch).await {
                Ok(count) => total += count,
                Err(err) => {
                    warn!(
                        err = tracing::field::debug(err),
                        count = total,
                        "failed to register existing chunks in index"
                    );
                    return;
                }
            }
        }
    }

    pub(super) async fn refresh(&self, spread_over: Duration) -> anyhow::Result<usize> {
        match self.chunk_index.refresh_registered(spread_over).await? {
            Some(total) => Ok(total),
            None => {
                let mut total = 0usize;
                let mut cursor = None;
                loop {
                    let batch = self
                        .read_chunk_batch(cursor, super::super::INDEX_SYNC_BATCH_SIZE)
                        .await?;
                    if batch.is_empty() {
                        return Ok(total);
                    }
                    cursor = batch.last().copied();
                    self.chunk_index.register_batch(&batch).await?;
                    total += batch.len();
                }
            }
        }
    }

    async fn repair(&self, repair_cursor: &mut Option<CheckSum>) -> anyhow::Result<Option<usize>> {
        let batch = self
            .read_chunk_batch(*repair_cursor, super::super::INDEX_REPAIR_BATCH_SIZE)
            .await?;
        if batch.is_empty() {
            *repair_cursor = None;
            return Ok(None);
        }
        *repair_cursor = batch.last().copied();
        self.chunk_index.repair_missing_owners(&batch).await
    }

    pub(super) async fn run_refresh_loop(
        self: Arc<Self>,
        refresh_interval: Duration,
        shutdown: ShutdownHandle,
    ) {
        let mut repair_cursor = None;
        let jitter = {
            let mut rng = rand::thread_rng();
            let max_jitter_ms = refresh_interval.as_millis().min(u128::from(u64::MAX)) as u64;
            Duration::from_millis(rng.gen_range(0..max_jitter_ms))
        };
        if !jitter.is_zero() {
            tokio::select! {
                _ = shutdown.wait() => return,
                _ = tokio::time::sleep(jitter) => {}
            }
        }
        let mut ticker = interval(refresh_interval);
        ticker.tick().await;
        loop {
            tokio::select! {
                _ = shutdown.wait() => break,
                _ = ticker.tick() => {
                    match self.refresh(refresh_interval).await {
                        Ok(total) => info!(count = total, "refreshed chunk index registrations"),
                        Err(err) => warn!(
                            err = tracing::field::debug(err),
                            "failed to refresh chunk index registrations"
                        ),
                    }

                    match self.repair(&mut repair_cursor).await {
                        Ok(Some(repaired)) if repaired > 0 => {
                            info!(count = repaired, "repaired missing chunk index registrations");
                        }
                        Ok(_) => {}
                        Err(err) => warn!(
                            err = tracing::field::debug(err),
                            "failed to repair chunk index registrations"
                        ),
                    }
                }
            }
        }
    }
}
