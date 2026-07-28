use crate::backend::slow_write_txn;
use crate::utils::now_epoch_secs;
use heed::{Env, MdbError};
use std::collections::{HashSet, VecDeque};
use std::io::Write;
use std::sync::{mpsc, Arc, Condvar, Mutex};
use std::time::Duration;
use tracing::{debug, warn};

use super::{
    AccessKey, CheckSum, CheckSumOnDisk, ChunkAccessDb, ChunkAccessIndexDb, ChunkDB, ChunkDataDb,
    ChunkIndexControl, GcDeleteResult, ACCESS_BATCH_SIZE, ACCESS_REFRESH_INTERVAL_SECS,
    ADD_CHUNKS_BATCH_SIZE, GC_BATCH,
};

pub(super) enum WriteRequest {
    AddChunk {
        cs: CheckSum,
        data: Vec<u8>,
        reply: mpsc::SyncSender<anyhow::Result<()>>,
    },
    AddChunksBatch {
        chunks: Vec<(CheckSum, Vec<u8>)>,
        reply: mpsc::SyncSender<anyhow::Result<()>>,
    },
    DeleteKeys {
        keys: Vec<AccessKey>,
        reply: mpsc::SyncSender<anyhow::Result<GcDeleteResult>>,
    },
}

#[derive(Default)]
pub(super) struct WriteQueue {
    high: VecDeque<WriteRequest>,
    medium: VecDeque<WriteRequest>,
    access_pending: HashSet<CheckSumOnDisk>,
    pub(super) stop: bool,
}

pub(super) struct WriterChannel {
    pub(super) inner: Mutex<WriteQueue>,
    pub(super) cond: Condvar,
}

impl Default for WriterChannel {
    fn default() -> Self {
        Self {
            inner: Mutex::new(WriteQueue::default()),
            cond: Condvar::new(),
        }
    }
}

impl WriterChannel {
    pub(super) fn submit_high(&self, req: WriteRequest) {
        let mut q = self.inner.lock().unwrap();
        q.high.push_back(req);
        drop(q);
        self.cond.notify_one();
    }

    pub(super) fn submit_medium(&self, req: WriteRequest) {
        let mut q = self.inner.lock().unwrap();
        q.medium.push_back(req);
        drop(q);
        self.cond.notify_one();
    }

    pub(super) fn enqueue_access(&self, cs: CheckSumOnDisk) {
        let mut q = self.inner.lock().unwrap();
        let inserted = q.access_pending.insert(cs);
        drop(q);
        if inserted {
            self.cond.notify_one();
        }
    }

    pub(super) fn enqueue_access_many<I: IntoIterator<Item = CheckSumOnDisk>>(&self, checksums: I) {
        let mut q = self.inner.lock().unwrap();
        let mut any = false;
        for cs in checksums {
            if q.access_pending.insert(cs) {
                any = true;
            }
        }
        drop(q);
        if any {
            self.cond.notify_one();
        }
    }
}

pub(super) struct WriterThread {
    env: Env,
    data_db: ChunkDataDb,
    access_db: ChunkAccessDb,
    access_index: ChunkAccessIndexDb,
    index_ctl: Option<Arc<dyn ChunkIndexControl>>,
    channel: Arc<WriterChannel>,
}

impl WriterThread {
    pub(super) fn new(
        env: Env,
        data_db: ChunkDataDb,
        access_db: ChunkAccessDb,
        access_index: ChunkAccessIndexDb,
        index_ctl: Option<Arc<dyn ChunkIndexControl>>,
        channel: Arc<WriterChannel>,
    ) -> Self {
        Self {
            env,
            data_db,
            access_db,
            access_index,
            index_ctl,
            channel,
        }
    }

    pub(super) fn run(self) {
        let refresh = Duration::from_secs(ACCESS_REFRESH_INTERVAL_SECS);
        loop {
            let (high, medium, access, should_stop) = {
                let mut q = self.channel.inner.lock().unwrap();
                if q.high.is_empty()
                    && q.medium.is_empty()
                    && q.access_pending.is_empty()
                    && !q.stop
                {
                    let (guard, _) = self.channel.cond.wait_timeout(q, refresh).unwrap();
                    q = guard;
                }
                let high: Vec<WriteRequest> = q.high.drain(..).collect();
                let medium: Vec<WriteRequest> = q.medium.drain(..).collect();
                let access: Vec<CheckSumOnDisk> = q.access_pending.drain().collect();
                let stop = q.stop;
                (high, medium, access, stop)
            };

            self.process_high(high);
            self.process_medium(medium);
            self.process_access_updates(&access);

            if should_stop {
                break;
            }
        }
    }

    fn process_high(&self, requests: Vec<WriteRequest>) {
        for req in requests {
            match req {
                WriteRequest::AddChunk { cs, data, reply } => {
                    let result = self.do_add_chunk(&cs, &data);
                    let _ = reply.send(result);
                }
                WriteRequest::AddChunksBatch { chunks, reply } => {
                    let result = self.do_add_chunks_batch(&chunks);
                    let _ = reply.send(result);
                }
                _ => unreachable!(),
            }
        }
    }

    fn process_medium(&self, requests: Vec<WriteRequest>) {
        for req in requests {
            match req {
                WriteRequest::DeleteKeys { keys, reply } => {
                    let result = self.do_delete_keys(keys);
                    let _ = reply.send(result);
                }
                _ => unreachable!(),
            }
        }
    }

    fn process_access_updates(&self, checksums: &[CheckSumOnDisk]) {
        if checksums.is_empty() {
            return;
        }
        let now = now_epoch_secs();
        for batch in checksums.chunks(ACCESS_BATCH_SIZE) {
            match slow_write_txn(&self.env, "WriterThread::access_update") {
                Ok(mut wtxn) => {
                    for cs in batch {
                        if let Err(e) = ChunkDB::update_access(
                            &self.access_db,
                            &self.access_index,
                            &mut wtxn,
                            cs,
                            now,
                        ) {
                            warn!(err = debug(e), "Failed to update chunk access time.");
                        }
                    }
                    if let Err(e) = wtxn.commit() {
                        warn!(err = debug(e), "Failed to commit chunk access updates.");
                    }
                }
                Err(e) => {
                    warn!(err = debug(e), "Failed to start chunk access update txn.");
                }
            }
        }
    }

    fn do_add_chunk(&self, cs: &CheckSum, data: &[u8]) -> anyhow::Result<()> {
        let cs_on_disk = CheckSumOnDisk::from(*cs);
        loop {
            let mut wtxn = slow_write_txn(&self.env, "WriterThread::add_chunk")?;
            match self
                .data_db
                .get_or_put_reserved(&mut wtxn, &cs_on_disk, data.len(), |reserved| {
                    reserved.write_all(data)
                }) {
                Ok(None) | Ok(Some(_)) => {
                    wtxn.commit()?;
                    if let Some(ctl) = &self.index_ctl {
                        let _ = ctl.register_chunk(cs);
                    }
                    self.channel.enqueue_access(cs_on_disk);
                    return Ok(());
                }
                Err(heed::Error::Mdb(MdbError::MapFull)) => {
                    drop(wtxn);
                    let removed = self.gc_lru_direct(GC_BATCH)?;
                    if removed == 0 {
                        return Err(heed::Error::Mdb(MdbError::MapFull).into());
                    }
                }
                Err(e) => return Err(e.into()),
            }
        }
    }

    fn do_add_chunks_batch(&self, chunks: &[(CheckSum, Vec<u8>)]) -> anyhow::Result<()> {
        for batch in chunks.chunks(ADD_CHUNKS_BATCH_SIZE) {
            self.do_add_chunks_sub_batch(batch)?;
        }
        Ok(())
    }

    fn do_add_chunks_sub_batch(&self, batch: &[(CheckSum, Vec<u8>)]) -> anyhow::Result<()> {
        loop {
            let mut wtxn = slow_write_txn(&self.env, "WriterThread::add_chunks")?;
            let mut map_full = false;
            for (cs, data) in batch {
                let cs_on_disk = CheckSumOnDisk::from(*cs);
                match self.data_db.get_or_put_reserved(
                    &mut wtxn,
                    &cs_on_disk,
                    data.len(),
                    |reserved| reserved.write_all(data),
                ) {
                    Ok(None) | Ok(Some(_)) => {}
                    Err(heed::Error::Mdb(MdbError::MapFull)) => {
                        map_full = true;
                        break;
                    }
                    Err(e) => return Err(e.into()),
                }
            }
            if !map_full {
                wtxn.commit()?;
                if let Some(ctl) = &self.index_ctl {
                    let checksums: Vec<CheckSum> = batch.iter().map(|(cs, _)| *cs).collect();
                    let _ = ctl.register_chunks(&checksums);
                }
                self.channel
                    .enqueue_access_many(batch.iter().map(|(cs, _)| CheckSumOnDisk::from(*cs)));
                return Ok(());
            } else {
                drop(wtxn);
                let removed = self.gc_lru_direct(GC_BATCH)?;
                if removed == 0 {
                    return Err(heed::Error::Mdb(MdbError::MapFull).into());
                }
            }
        }
    }

    fn do_delete_keys(&self, keys: Vec<AccessKey>) -> anyhow::Result<GcDeleteResult> {
        if keys.is_empty() {
            return Ok(GcDeleteResult {
                removed: 0,
                checksums: Vec::new(),
            });
        }
        let mut wtxn = slow_write_txn(&self.env, "WriterThread::delete_keys")?;
        let mut checksums = Vec::with_capacity(keys.len());
        for key in &keys {
            checksums.push(CheckSum::from(key.cs));
            if let Err(e) = self.data_db.delete(&mut wtxn, &key.cs) {
                warn!(err = debug(e), "Failed to delete chunk data.");
            }
            if let Err(e) = self.access_db.delete(&mut wtxn, &key.cs) {
                warn!(err = debug(e), "Failed to delete chunk access metadata.");
            }
            if let Err(e) = self.access_index.delete(&mut wtxn, key) {
                warn!(err = debug(e), "Failed to delete chunk access index.");
            }
        }
        wtxn.commit()?;
        Ok(GcDeleteResult {
            removed: keys.len(),
            checksums,
        })
    }

    fn gc_lru_direct(&self, max_delete: usize) -> anyhow::Result<usize> {
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
        if targets.is_empty() {
            return Ok(0);
        }
        let checksums: Vec<CheckSum> = targets.iter().map(|k| CheckSum::from(k.cs)).collect();
        let mut wtxn = slow_write_txn(&self.env, "WriterThread::gc_lru_direct")?;
        for key in &targets {
            let _ = self.data_db.delete(&mut wtxn, &key.cs);
            let _ = self.access_db.delete(&mut wtxn, &key.cs);
            let _ = self.access_index.delete(&mut wtxn, key);
        }
        wtxn.commit()?;
        if let Some(ctl) = &self.index_ctl {
            if !ctl.unregister_chunks(&checksums) {
                debug!(
                    count = checksums.len(),
                    "Failed to unregister chunks from index during MapFull GC."
                );
            }
        }
        Ok(targets.len())
    }
}
