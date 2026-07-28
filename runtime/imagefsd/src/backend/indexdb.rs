use crate::backend::chunkdb::{CheckSum, CheckSumOnDisk, LMDB_MAX_READERS, MAX_DBS};
use bytemuck::{Pod, Zeroable};
use heed::{BoxedError, BytesDecode, BytesEncode, Database, Env, EnvOpenOptions};
use std::borrow::Cow;
use std::cmp::Ordering;
use std::path::Path;
use std::time::Instant;
use tracing::{info, warn};

const META_DB_NAME: &str = "dedup_info";
const META_DB_SIZE: usize = 100 * 1024 * 1024_usize;
pub(crate) const DEFAULT_DATA_ID: [u8; 32] = [0_u8; 32];

#[derive(Debug)]
pub struct IndexDB {
    pub(crate) env: Env,
    pub(crate) storage: Database<DedupRange, DedupInfo>,
}

impl IndexDB {
    pub fn open<P: AsRef<Path>>(path: P) -> anyhow::Result<Self> {
        let open_begin = Instant::now();
        let env = unsafe {
            EnvOpenOptions::new()
                .map_size(META_DB_SIZE)
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
            "IndexDB opened LMDB environment"
        );
        if actual_max_readers < LMDB_MAX_READERS {
            warn!(
                requested_max_readers = LMDB_MAX_READERS,
                actual_max_readers,
                "IndexDB LMDB max_readers is lower than requested; restart all processes sharing this env to apply the new limit."
            );
        }

        // Try read transaction first to avoid blocking on the write mutex.
        let t = std::time::Instant::now();
        let rtxn = env.read_txn()?;
        let storage = env.open_database::<DedupRange, DedupInfo>(&rtxn, Some(META_DB_NAME))?;
        rtxn.commit()?;

        let storage = match storage {
            Some(db) => {
                info!(
                    "IndexDB opened existing database via read txn in {:?}",
                    t.elapsed()
                );
                db
            }
            None => {
                info!("IndexDB database not found, creating via write txn");
                let mut wtxn = super::slow_write_txn(&env, "IndexDB::open")?;
                let db = env.create_database(&mut wtxn, Some(META_DB_NAME))?;
                wtxn.commit()?;
                info!(
                    "IndexDB created database via write txn in {:?}",
                    t.elapsed()
                );
                db
            }
        };

        Ok(Self { env, storage })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    #[test]
    fn test_open_sets_configured_max_readers() {
        let temp = TempDir::new().unwrap();
        let db = IndexDB::open(temp.path()).unwrap();

        assert_eq!(db.env.max_readers(), LMDB_MAX_READERS);
    }
}

#[repr(C)]
#[derive(Debug, Eq, PartialEq, Copy, Clone, Pod, Zeroable)]
pub(crate) struct DedupRange {
    pub(crate) id: [u8; 32],
    pub(crate) start: u64,
    pub(crate) end: u64,
}

impl DedupRange {
    pub(crate) fn new(off: u64, len: u32) -> Self {
        Self::new_with_id(DEFAULT_DATA_ID, off, len)
    }

    pub(crate) fn new_with_id(id: [u8; 32], off: u64, len: u32) -> Self {
        Self {
            id,
            start: off,
            end: off + len as u64,
        }
    }

    pub(crate) fn size(&self) -> usize {
        (self.end - self.start) as usize
    }
}

impl Ord for DedupRange {
    fn cmp(&self, other: &Self) -> Ordering {
        if self.id != other.id {
            self.id.cmp(&other.id)
        } else if self.end != other.end {
            self.end.cmp(&other.end)
        } else {
            other.start.cmp(&self.start)
        }
    }
}

impl PartialOrd for DedupRange {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl<'a> BytesEncode<'a> for DedupRange {
    type EItem = DedupRange;

    fn bytes_encode(item: &'a Self::EItem) -> Result<Cow<'a, [u8]>, BoxedError> {
        let mut key = Vec::with_capacity(48);
        key.extend_from_slice(&item.id);
        key.extend_from_slice(&item.end.to_be_bytes());
        key.extend_from_slice(&item.start.to_be_bytes());
        Ok(Cow::Owned(key))
    }
}

impl<'a> BytesDecode<'a> for DedupRange {
    type DItem = DedupRange;

    fn bytes_decode(bytes: &'a [u8]) -> Result<Self::DItem, BoxedError> {
        if bytes.len() != 48 {
            return Err("Invalid DedupRange length".into());
        }
        let id: [u8; 32] = bytes[0..32].try_into()?;
        let end = u64::from_be_bytes(bytes[32..40].try_into()?);
        let start = u64::from_be_bytes(bytes[40..48].try_into()?);
        Ok(DedupRange { id, start, end })
    }
}

#[repr(C)]
#[derive(Debug, Copy, Clone, Eq, Pod, Zeroable)]
pub(crate) struct DedupInfo {
    pub(crate) off: u64,
    pub(crate) len: u32,
    reserved: u32,
    pub(crate) cs: CheckSumOnDisk,
}

impl Ord for DedupInfo {
    fn cmp(&self, other: &Self) -> Ordering {
        let r1 = DedupRange::new(self.off, self.len);
        let r2 = DedupRange::new(other.off, other.len);
        r1.cmp(&r2)
    }
}

impl PartialOrd for DedupInfo {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl PartialEq for DedupInfo {
    fn eq(&self, other: &Self) -> bool {
        self.off.eq(&other.off) && self.len.eq(&other.len) && self.cs.eq(&other.cs)
    }
}

impl DedupInfo {
    pub(crate) fn new(off: u64, len: u32, cs: CheckSum) -> Self {
        Self {
            off,
            len,
            reserved: 0,
            cs: CheckSumOnDisk::from(cs),
        }
    }

    pub(crate) fn end(&self) -> u64 {
        self.off + self.len as u64
    }
}

impl<'a> BytesEncode<'a> for DedupInfo {
    type EItem = DedupInfo;

    fn bytes_encode(item: &'a Self::EItem) -> Result<Cow<'a, [u8]>, BoxedError> {
        Ok(Cow::Borrowed(bytemuck::bytes_of(item)))
    }
}

impl<'a> BytesDecode<'a> for DedupInfo {
    type DItem = &'a DedupInfo;

    fn bytes_decode(bytes: &'a [u8]) -> Result<Self::DItem, BoxedError> {
        Ok(bytemuck::from_bytes(bytes))
    }
}
