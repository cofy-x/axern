use crate::backend::chunkdb::ChunkDB;
use crate::backend::dedup::DedupReader;
use crate::backend::indexdb::IndexDB;
use crate::backend::peer::LocalChunkClient;
use crate::backend::{Backend, BackendEx};
use crate::image::FsReadMetrics;
#[cfg(target_os = "linux")]
use crate::utils::align_up;
#[cfg(target_os = "linux")]
use fuse_backend_rs::abi::fuse_abi::{stat64, Attr, FsOptions, ROOT_ID};
use fuse_backend_rs::api::filesystem::ZeroCopyWriter;
#[cfg(target_os = "linux")]
use fuse_backend_rs::api::filesystem::{Context, DirEntry, Entry};
#[cfg(target_os = "linux")]
use std::ffi::{CStr, OsStr};
use std::io::ErrorKind;
#[cfg(target_os = "linux")]
use std::os::unix::ffi::OsStrExt;
use std::sync::Arc;
#[cfg(target_os = "linux")]
use std::time::Duration;
#[cfg(target_os = "linux")]
use std::time::{Instant, SystemTime, UNIX_EPOCH};
#[cfg(not(target_os = "linux"))]
use std::time::{SystemTime, UNIX_EPOCH};

#[cfg(target_os = "linux")]
const IMAGE_FILE_ID: u64 = 2;
#[cfg(target_os = "linux")]
const ATTR_TIMEOUT: u64 = 1_u64 << 32;

#[derive(Debug)]
pub struct RawImage {
    name: String,
    size: u64,
    create_tm: u64,
    b: Arc<dyn Backend>,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    metrics: FsReadMetrics,
}

impl RawImage {
    pub fn new(
        name: &str,
        b: Arc<dyn BackendEx>,
        dedup_db: Option<(Arc<ChunkDB>, Arc<IndexDB>)>,
        local_chunk_client: Option<Arc<LocalChunkClient>>,
    ) -> anyhow::Result<Self> {
        let size = b.size();
        let backend: Arc<dyn Backend> = match dedup_db {
            Some((chunk_db, index_db)) => {
                let dedup = DedupReader::new(b, chunk_db, index_db, name, local_chunk_client)?;
                let raw = Arc::new(RawDedupImage::new(dedup)?);
                raw as Arc<dyn Backend>
            }
            None => b as Arc<dyn Backend>,
        };
        let create_tm = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();
        let image = Self {
            name: name.to_string(),
            size,
            create_tm,
            b: backend,
            metrics: FsReadMetrics::new("raw"),
        };
        #[cfg(not(target_os = "linux"))]
        image.keep_fields_live();
        Ok(image)
    }

    #[cfg(not(target_os = "linux"))]
    fn keep_fields_live(&self) {
        let _ = (&self.name, self.create_tm);
    }

    #[cfg(target_os = "linux")]
    fn root_attr(&self) -> Attr {
        Attr {
            ino: ROOT_ID,
            size: 4096,
            blocks: 8,
            atime: self.create_tm,
            mtime: self.create_tm,
            ctime: self.create_tm,
            atimensec: 0,
            mtimensec: 0,
            ctimensec: 0,
            mode: libc::S_IFDIR | 0o755,
            nlink: 2,
            uid: 0,
            gid: 0,
            rdev: 0,
            blksize: 4096,
            flags: 0,
        }
    }

    #[cfg(target_os = "linux")]
    fn image_attr(&self) -> Attr {
        Attr {
            ino: IMAGE_FILE_ID,
            size: self.size,
            blocks: align_up(self.size, 4096) / 512,
            atime: self.create_tm,
            mtime: self.create_tm,
            ctime: self.create_tm,
            atimensec: 0,
            mtimensec: 0,
            ctimensec: 0,
            mode: libc::S_IFREG | 0o644,
            nlink: 1,
            uid: 0,
            gid: 0,
            rdev: 0,
            blksize: 4096,
            flags: 0,
        }
    }

    #[cfg(target_os = "linux")]
    fn get_inode_entry(&self, ino: u64) -> Option<Entry> {
        match ino {
            ROOT_ID => Some(Entry {
                inode: ROOT_ID,
                generation: ROOT_ID,
                attr: self.root_attr().into(),
                attr_flags: 0,
                attr_timeout: Duration::from_secs(ATTR_TIMEOUT),
                entry_timeout: Duration::from_secs(ATTR_TIMEOUT),
            }),
            IMAGE_FILE_ID => Some(Entry {
                inode: IMAGE_FILE_ID,
                generation: IMAGE_FILE_ID,
                attr: self.image_attr().into(),
                attr_flags: 0,
                attr_timeout: Duration::from_secs(ATTR_TIMEOUT),
                entry_timeout: Duration::from_secs(ATTR_TIMEOUT),
            }),
            _ => None,
        }
    }
}

impl Backend for RawImage {
    fn size(&self) -> u64 {
        self.size
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        self.b.fetch(off, data)
    }

    fn write_to_fuse_writer(
        &self,
        off: usize,
        size: u32,
        w: &mut dyn ZeroCopyWriter,
    ) -> std::io::Result<usize> {
        self.b.write_to_fuse_writer(off, size, w)
    }
}

#[cfg(target_os = "linux")]
impl fuse_backend_rs::api::filesystem::FileSystem for RawImage {
    type Inode = u64;
    type Handle = u64;

    fn init(&self, capable: FsOptions) -> std::io::Result<FsOptions> {
        let mut opts = FsOptions::empty();
        opts.insert(FsOptions::ASYNC_DIO);
        opts.insert(FsOptions::ASYNC_READ);
        opts.insert(FsOptions::MAX_PAGES);
        if capable.contains(FsOptions::ZERO_MESSAGE_OPEN) {
            opts.insert(FsOptions::ZERO_MESSAGE_OPEN);
        }
        if capable.contains(FsOptions::ZERO_MESSAGE_OPENDIR) {
            opts.insert(FsOptions::ZERO_MESSAGE_OPENDIR);
        }

        Ok(opts)
    }

    fn lookup(&self, _ctx: &Context, parent: Self::Inode, name: &CStr) -> std::io::Result<Entry> {
        let name = OsStr::from_bytes(name.to_bytes());
        if parent != ROOT_ID {
            return Err(std::io::Error::from_raw_os_error(libc::ENOENT));
        }
        if name == "." || name == ".." {
            return Ok(self.get_inode_entry(ROOT_ID).unwrap());
        }
        if name.eq(self.name.as_str()) {
            return Ok(self.get_inode_entry(IMAGE_FILE_ID).unwrap());
        }
        Err(std::io::Error::from_raw_os_error(libc::ENOENT))
    }

    fn getattr(
        &self,
        _ctx: &Context,
        inode: Self::Inode,
        _handle: Option<Self::Handle>,
    ) -> std::io::Result<(stat64, Duration)> {
        if let Some(e) = self.get_inode_entry(inode) {
            Ok((e.attr, Duration::from_secs(ATTR_TIMEOUT)))
        } else {
            Err(std::io::Error::from_raw_os_error(libc::ENOENT))
        }
    }

    fn read(
        &self,
        _ctx: &Context,
        inode: Self::Inode,
        _handle: Self::Handle,
        w: &mut dyn ZeroCopyWriter,
        mut size: u32,
        offset: u64,
        _lock_owner: Option<u64>,
        _flags: u32,
    ) -> std::io::Result<usize> {
        let begin = Instant::now();
        if inode != IMAGE_FILE_ID {
            self.metrics.record_read("error", 0.0, 0);
            return Err(std::io::Error::from_raw_os_error(libc::EINVAL));
        }
        if size == 0 {
            self.metrics.record_read("ok", 0.0, 0);
            return Ok(0);
        }
        if offset >= self.size {
            self.metrics.record_read("ok", 0.0, 0);
            return Ok(0);
        }
        if offset + size as u64 > self.size {
            size = (self.size - offset) as u32;
        }
        let result = self.b.write_to_fuse_writer(offset as usize, size, w);
        let (status, bytes) = match &result {
            Ok(n) => ("ok", *n),
            Err(_) => ("error", 0),
        };
        self.metrics
            .record_read(status, begin.elapsed().as_secs_f64() * 1000.0, bytes);
        result
    }

    fn readdir(
        &self,
        _ctx: &Context,
        inode: Self::Inode,
        _handle: Self::Handle,
        size: u32,
        offset: u64,
        add_entry: &mut dyn FnMut(DirEntry) -> std::io::Result<usize>,
    ) -> std::io::Result<()> {
        if inode != ROOT_ID {
            return Err(std::io::Error::from_raw_os_error(libc::EINVAL));
        }
        if size == 0 || offset >= 3 {
            return Ok(());
        }
        if offset < 1 {
            add_entry(DirEntry {
                ino: ROOT_ID,
                offset: 1,
                type_: 0,
                name: ".".as_bytes(),
            })?;
        }
        if offset < 2 {
            add_entry(DirEntry {
                ino: ROOT_ID,
                offset: 2,
                type_: 0,
                name: "..".as_bytes(),
            })?;
        }
        let entry = DirEntry {
            ino: IMAGE_FILE_ID,
            offset: 3,
            type_: 0,
            name: self.name.as_bytes(),
        };
        add_entry(entry)?;
        Ok(())
    }
}

#[derive(Debug)]
pub struct RawDedupImage {
    inner: Arc<DedupReader>,
}

impl RawDedupImage {
    pub fn new(dedup: DedupReader) -> anyhow::Result<Self> {
        let inner = Arc::new(dedup);
        Ok(Self { inner })
    }

    fn validate_range(&self, off: usize, mut size: usize) -> std::io::Result<(usize, usize)> {
        let len = self.size() as usize;
        if off > len {
            Err(std::io::Error::new(
                ErrorKind::InvalidInput,
                "Invalid offset",
            ))
        } else {
            if off == len && size == 0 {
                return Ok((off, 0));
            }
            if off + size > len {
                size = len - off;
            }
            Ok((off, size))
        }
    }
}

impl Backend for RawDedupImage {
    fn size(&self) -> u64 {
        self.inner.size()
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        let (off, len) = self.validate_range(off, data.len())?;
        if len == 0 {
            return Ok(0);
        }
        self.inner.fetch(off, &mut data[..len])
    }

    fn write_to_fuse_writer(
        &self,
        off: usize,
        size: u32,
        w: &mut dyn ZeroCopyWriter,
    ) -> std::io::Result<usize> {
        let (off, len) = self.validate_range(off, size as usize)?;
        if len == 0 {
            return Ok(0);
        }
        self.inner.write_to_fuse_writer(off, len as u32, w)
    }
}

#[cfg(test)]
mod tests;
