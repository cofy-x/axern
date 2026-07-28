use crate::backend::chunkdb::ChunkDB;
use crate::backend::general::GeneralBackend;
use crate::backend::indexdb::IndexDB;
use crate::backend::peer::LocalChunkClient;
use crate::image::FsReadMetrics;
#[cfg(target_os = "linux")]
use fuse_backend_rs::abi::fuse_abi::statvfs64;
#[cfg(target_os = "linux")]
use fuse_backend_rs::abi::fuse_abi::{stat64, Attr, FsOptions, ROOT_ID};
#[cfg(target_os = "linux")]
use fuse_backend_rs::api::filesystem::{
    Context, DirEntry, Entry, GetxattrReply, ListxattrReply, OpenOptions, ZeroCopyWriter,
};
use nydus_api::ConfigV2;
use nydus_rafs::metadata::RafsSuper;
#[cfg(target_os = "linux")]
use nydus_rafs::metadata::{RafsInode, RafsInodeExt};
#[cfg(target_os = "linux")]
use nydus_rafs::metadata::{RafsInodeWalkAction, DOT, DOTDOT};
use nydus_storage::device::BlobInfo;
use std::collections::HashMap;
#[cfg(not(target_os = "linux"))]
use std::ffi::OsString;
#[cfg(target_os = "linux")]
use std::ffi::{CStr, OsStr, OsString};
use std::io::{self, ErrorKind};
#[cfg(target_os = "linux")]
use std::os::unix::ffi::OsStrExt;
use std::path::Path;
use std::sync::atomic::Ordering::Relaxed;
use std::sync::{Arc, Mutex};
#[cfg(target_os = "linux")]
use std::time::Duration;
#[cfg(target_os = "linux")]
use std::time::Instant;
use std::time::{SystemTime, UNIX_EPOCH};
#[cfg(target_os = "linux")]
use tracing::error;
use tracing::info;
#[path = "nydus/blob_io.rs"]
mod blob_io;
#[path = "nydus/decoded_cache.rs"]
mod decoded_cache;
#[path = "nydus/store.rs"]
mod store;

use blob_io::BlobIo;
use store::ChunkStoreQueue;

pub(crate) struct NydusCacheConfig {
    readahead_workers: usize,
    readahead_window_bytes: usize,
    decoded_cache_bytes: usize,
    node_id: String,
}

impl NydusCacheConfig {
    pub(crate) fn new(
        readahead_workers: usize,
        readahead_window_bytes: usize,
        decoded_cache_bytes: usize,
        node_id: String,
    ) -> Self {
        Self {
            readahead_workers,
            readahead_window_bytes,
            decoded_cache_bytes,
            node_id,
        }
    }
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
#[derive(Clone)]
struct CachedInodePath {
    parent_ino: u64,
    name: OsString,
}

pub struct NydusImage {
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    sb: Arc<RafsSuper>,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    blobs: HashMap<u32, Arc<BlobInfo>>,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    inode_paths: Mutex<HashMap<u64, CachedInodePath>>,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    blob_io: BlobIo,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    chunk_store: Option<ChunkStoreQueue>,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    i_uid: u32,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    i_gid: u32,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    i_time: u64,
    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    metrics: FsReadMetrics,
}

impl NydusImage {
    pub(crate) fn new<P: AsRef<Path>>(
        bootstrap: P,
        backend_cfg: P,
        cache_dir: &str,
        dedup_db: Option<(Arc<ChunkDB>, Arc<IndexDB>)>,
        local_chunk_client: Option<Arc<LocalChunkClient>>,
        cache_config: NydusCacheConfig,
    ) -> anyhow::Result<Self> {
        if !backend_cfg.as_ref().is_file() {
            return Err(io::Error::new(ErrorKind::InvalidInput, "invalid backend cfg").into());
        }
        let mut config = ConfigV2::default();
        config.internal.blob_accessible.store(true, Relaxed);
        let t = std::time::Instant::now();
        let backend = Arc::new(GeneralBackend::new(backend_cfg)?);
        info!("GeneralBackend::new took {:?}", t.elapsed());
        config.backend = Some(backend.backend_config().clone());
        let t = std::time::Instant::now();
        let (sb, _reader) = RafsSuper::load_from_file(bootstrap, Arc::new(config), false)?;
        info!("RafsSuper::load_from_file took {:?}", t.elapsed());
        let mut blobs = HashMap::new();
        for blob in sb.superblock.get_blob_infos() {
            blobs.insert(blob.blob_index(), blob);
        }
        let i_time = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();
        let i_uid = unsafe { libc::geteuid() } as u32;
        let i_gid = unsafe { libc::getegid() } as u32;
        let blob_io = BlobIo::new(
            backend,
            cache_dir.to_string(),
            dedup_db.clone(),
            local_chunk_client,
            cache_config,
        )?;
        let chunk_store = dedup_db.as_ref().map(|(chunk_db, _)| {
            ChunkStoreQueue::new(chunk_db.clone(), blob_io.decoded_chunk_cache())
        });

        Ok(Self {
            sb: Arc::new(sb),
            blobs,
            inode_paths: Mutex::new(HashMap::new()),
            blob_io,
            chunk_store,
            i_uid,
            i_gid,
            i_time,
            metrics: FsReadMetrics::new("nydus"),
        })
    }

    #[cfg(target_os = "linux")]
    fn root_ino(&self) -> u64 {
        self.sb.superblock.root_ino()
    }

    #[cfg(target_os = "linux")]
    fn to_rafs_ino(&self, ino: u64) -> u64 {
        if ino == ROOT_ID {
            self.root_ino()
        } else {
            ino
        }
    }

    #[cfg(target_os = "linux")]
    fn to_fuse_ino(&self, ino: u64) -> u64 {
        if ino == self.root_ino() {
            ROOT_ID
        } else {
            ino
        }
    }

    #[cfg(target_os = "linux")]
    fn get_blob(&self, index: u32) -> io::Result<Arc<BlobInfo>> {
        self.blobs
            .get(&index)
            .cloned()
            .ok_or_else(|| io::Error::new(ErrorKind::NotFound, "blob index not found"))
    }

    #[cfg(target_os = "linux")]
    fn remember_inode_path(&self, parent_ino: u64, child_ino: u64, name: &OsStr) {
        if name == DOT || name == DOTDOT {
            return;
        }
        let mut inode_paths = self.inode_paths.lock().unwrap();
        inode_paths.insert(
            self.to_fuse_ino(child_ino),
            CachedInodePath {
                parent_ino: self.to_rafs_ino(parent_ino),
                name: name.to_os_string(),
            },
        );
    }

    #[cfg(target_os = "linux")]
    fn get_read_inode(&self, ino: u64) -> io::Result<Arc<dyn RafsInodeExt>> {
        let rafs_ino = self.to_rafs_ino(ino);
        match self.sb.get_extended_inode(rafs_ino, false) {
            Ok(inode) => Ok(inode),
            Err(base_err) => {
                let cached = self.inode_paths.lock().unwrap().get(&ino).cloned();
                let Some(cached) = cached else {
                    return Err(base_err);
                };
                let parent = self.sb.get_extended_inode(cached.parent_ino, false)?;
                let inode = parent.get_child_by_name(cached.name.as_os_str())?;
                if inode.ino() != rafs_ino {
                    return Err(io::Error::new(
                        ErrorKind::InvalidData,
                        format!(
                            "cached inode mismatch: expected {}, got {} for {:?}",
                            rafs_ino,
                            inode.ino(),
                            cached.name
                        ),
                    ));
                }
                Ok(inode)
            }
        }
    }

    #[cfg(target_os = "linux")]
    fn get_inode_entry(&self, inode: &dyn RafsInode) -> Entry {
        let mut entry = inode.get_entry();
        let fuse_ino = self.to_fuse_ino(entry.inode);
        entry.inode = fuse_ino;
        entry.generation = fuse_ino;
        entry.attr.st_ino = fuse_ino;
        if !self.sb.meta.explicit_uidgid() {
            entry.attr.st_uid = self.i_uid;
            entry.attr.st_gid = self.i_gid;
        }
        if entry.attr.st_mtime == 0 {
            entry.attr.st_atime = self.i_time as i64;
            entry.attr.st_ctime = self.i_time as i64;
            entry.attr.st_mtime = self.i_time as i64;
        }
        entry.attr_timeout = self.sb.meta.attr_timeout;
        entry.entry_timeout = self.sb.meta.entry_timeout;
        entry
    }

    #[cfg(target_os = "linux")]
    fn get_inode_attr(&self, ino: u64) -> io::Result<Attr> {
        let inode = self.sb.get_extended_inode(self.to_rafs_ino(ino), false)?;
        let mut attr = inode.get_attr();
        attr.ino = self.to_fuse_ino(attr.ino);
        if !self.sb.meta.explicit_uidgid() {
            attr.uid = self.i_uid;
            attr.gid = self.i_gid;
        }
        if attr.mtime == 0 {
            attr.atime = self.i_time;
            attr.ctime = self.i_time;
            attr.mtime = self.i_time;
        }
        Ok(attr)
    }

    #[cfg(target_os = "linux")]
    fn do_readdir(
        &self,
        ino: u64,
        size: u32,
        offset: u64,
        add_entry: &mut dyn FnMut(DirEntry) -> io::Result<usize>,
    ) -> io::Result<()> {
        if size == 0 {
            return Ok(());
        }
        let parent = self.sb.get_inode(self.to_rafs_ino(ino), false)?;
        if !parent.is_dir() {
            return Err(io::Error::from_raw_os_error(libc::ENOTDIR));
        }

        let parent_ino = ino;
        let mut handler = |_inode, name: OsString, child_ino, offset| match add_entry(DirEntry {
            ino: self.to_fuse_ino(child_ino),
            offset,
            type_: 0,
            name: name.as_os_str().as_bytes(),
        }) {
            Ok(0) => {
                self.remember_inode_path(parent_ino, child_ino, name.as_os_str());
                Ok(RafsInodeWalkAction::Break)
            }
            Ok(_) => {
                self.remember_inode_path(parent_ino, child_ino, name.as_os_str());
                Ok(RafsInodeWalkAction::Continue)
            }
            Err(e) => Err(e),
        };

        parent.walk_children_inodes(offset, &mut handler)?;
        Ok(())
    }

    #[cfg(target_os = "linux")]
    fn xattr_supported(&self) -> bool {
        self.sb.meta.has_xattr()
    }

    #[cfg(target_os = "linux")]
    fn check_read_range(&self, inode: &dyn RafsInode, size: u32, offset: u64) -> io::Result<u64> {
        if offset.checked_add(size as u64).is_none() {
            return Err(io::Error::new(ErrorKind::InvalidInput, "invalid range"));
        }
        if !inode.is_reg() {
            return Err(io::Error::from_raw_os_error(libc::EISDIR));
        }
        let inode_size = inode.size();
        if size == 0 || offset >= inode_size {
            return Ok(0);
        }
        Ok(std::cmp::min(size as u64, inode_size - offset))
    }

    #[allow(clippy::too_many_arguments)]
    #[cfg(target_os = "linux")]
    fn read_chunk_into_writer(
        &self,
        inode: &dyn RafsInodeExt,
        chunk_idx: u64,
        offset: u64,
        chunk_size: u64,
        remaining: &mut usize,
        total: &mut usize,
        w: &mut dyn ZeroCopyWriter,
    ) -> io::Result<()> {
        let chunk_info = inode.get_chunk_info(chunk_idx as u32).inspect_err(|e| {
            error!(
                err = debug(e),
                ino = inode.ino(),
                chunk_idx,
                "Failed to load Nydus chunk info."
            );
        })?;
        let blob = self.get_blob(chunk_info.blob_index()).inspect_err(|e| {
            error!(
                err = debug(e),
                ino = inode.ino(),
                chunk_idx,
                blob_index = chunk_info.blob_index(),
                "Failed to resolve Nydus blob for chunk."
            );
        })?;
        let chunk_base = chunk_idx * chunk_size;
        let chunk_offset = if offset > chunk_base {
            (offset - chunk_base) as usize
        } else {
            0
        };
        let chunk_uncompressed = chunk_info.uncompressed_size() as usize;
        if chunk_offset >= chunk_uncompressed {
            return Err(io::Error::new(
                ErrorKind::InvalidData,
                "invalid chunk offset",
            ));
        }
        let wanted = (*remaining).min(chunk_uncompressed - chunk_offset);
        tracing::debug!(
            ino = inode.ino(),
            chunk_idx,
            blob_id = blob.blob_id(),
            blob_index = chunk_info.blob_index(),
            chunk_compressed_offset = chunk_info.compressed_offset(),
            chunk_compressed_size = chunk_info.compressed_size(),
            chunk_uncompressed_size = chunk_info.uncompressed_size(),
            chunk_file_offset = chunk_base,
            chunk_offset,
            wanted,
            remaining = *remaining,
            "Reading Nydus chunk."
        );
        let cs = self
            .blob_io
            .checksum_from_chunk(chunk_info.as_ref(), &blob)
            .inspect_err(|e| {
                error!(
                    err = debug(e),
                    ino = inode.ino(),
                    chunk_idx,
                    blob_id = blob.blob_id(),
                    "Failed to compute checksum for Nydus chunk."
                );
            })?;

        if let Some(n) = self
            .blob_io
            .read_from_chunkdb(&cs, chunk_offset, wanted, w)?
        {
            *total += n;
            *remaining -= n;
            return Ok(());
        }

        let decoded = self
            .blob_io
            .read_chunk_data(cs, chunk_info.as_ref(), &blob)
            .inspect_err(|e| {
                error!(err = debug(e), "Failed to read chunk data.");
            })?;
        let end = chunk_offset + wanted;
        if end > decoded.bytes.len() {
            return Err(io::Error::new(
                ErrorKind::InvalidData,
                "chunk data is shorter than expected",
            ));
        }
        let n = w
            .write(&decoded.bytes[chunk_offset..end])
            .inspect_err(|e| {
                error!(
                    err = debug(e),
                    ino = inode.ino(),
                    chunk_idx,
                    blob_id = blob.blob_id(),
                    chunk_offset,
                    end,
                    data_len = decoded.bytes.len(),
                    available_bytes = w.available_bytes(),
                    "Failed to write Nydus chunk into FUSE response."
                );
            })?;
        *total += n;
        *remaining -= n;
        if decoded.bytes.len() == chunk_uncompressed {
            if let Some(store) = &self.chunk_store {
                store.enqueue(cs, Arc::clone(&decoded.bytes));
            }
        }
        Ok(())
    }
}

#[cfg(target_os = "linux")]
impl fuse_backend_rs::api::filesystem::FileSystem for NydusImage {
    type Inode = u64;
    type Handle = u64;

    fn init(&self, capable: FsOptions) -> io::Result<FsOptions> {
        let mut opts = FsOptions::empty();
        opts.insert(FsOptions::ASYNC_READ);
        opts.insert(FsOptions::ASYNC_DIO);
        opts.insert(FsOptions::BIG_WRITES);
        opts.insert(FsOptions::MAX_PAGES);
        if capable.contains(FsOptions::ZERO_MESSAGE_OPEN) {
            opts.insert(FsOptions::ZERO_MESSAGE_OPEN);
        }
        if capable.contains(FsOptions::ZERO_MESSAGE_OPENDIR) {
            opts.insert(FsOptions::ZERO_MESSAGE_OPENDIR);
        }
        Ok(opts)
    }

    fn lookup(&self, _ctx: &Context, parent: Self::Inode, name: &CStr) -> io::Result<Entry> {
        let target = OsStr::from_bytes(name.to_bytes());
        let parent_inode = self.sb.get_inode(self.to_rafs_ino(parent), false)?;
        if !parent_inode.is_dir() {
            return Err(io::Error::from_raw_os_error(libc::ENOTDIR));
        }

        if target == DOT || (parent == ROOT_ID && target == DOTDOT) {
            return Ok(self.get_inode_entry(parent_inode.as_ref()));
        }
        if target == DOTDOT {
            let parent = self.sb.get_extended_inode(parent_inode.ino(), false)?;
            let ino = parent.parent();
            let inode = self.sb.get_inode(ino, false)?;
            return Ok(self.get_inode_entry(inode.as_ref()));
        }

        match parent_inode.get_child_by_name(target) {
            Ok(inode) => {
                self.remember_inode_path(parent, inode.ino(), target);
                Ok(self.get_inode_entry(inode.as_inode()))
            }
            Err(_) => Err(io::Error::from_raw_os_error(libc::ENOENT)),
        }
    }

    fn getattr(
        &self,
        _ctx: &Context,
        inode: Self::Inode,
        _handle: Option<Self::Handle>,
    ) -> io::Result<(stat64, Duration)> {
        let attr = self.get_inode_attr(inode)?;
        Ok((attr.into(), self.sb.meta.attr_timeout))
    }

    fn readlink(&self, _ctx: &Context, ino: u64) -> io::Result<Vec<u8>> {
        let inode = self.sb.get_inode(self.to_rafs_ino(ino), false)?;
        let target = inode.get_symlink()?;
        Ok(target.as_bytes().to_vec())
    }

    fn open(
        &self,
        _ctx: &Context,
        _inode: Self::Inode,
        _flags: u32,
        _fuse_flags: u32,
    ) -> io::Result<(Option<Self::Handle>, OpenOptions, Option<u32>)> {
        Ok((None, OpenOptions::KEEP_CACHE, None))
    }

    fn read(
        &self,
        _ctx: &Context,
        ino: Self::Inode,
        _handle: Self::Handle,
        w: &mut dyn ZeroCopyWriter,
        size: u32,
        offset: u64,
        _lock_owner: Option<u64>,
        _flags: u32,
    ) -> io::Result<usize> {
        let begin = Instant::now();
        let result = (|| -> io::Result<usize> {
            let rafs_ino = self.to_rafs_ino(ino);
            let inode = self.get_read_inode(ino).inspect_err(|e| {
                error!(
                    err = debug(e),
                    ino, rafs_ino, "Failed to resolve extended Nydus inode for read."
                );
            })?;
            let read_size = self.check_read_range(inode.as_inode(), size, offset)?;
            if read_size == 0 {
                return Ok(0);
            }
            let chunk_size = self.sb.meta.chunk_size as u64;
            let start_chunk = offset / chunk_size;
            let end_chunk = (offset + read_size - 1) / chunk_size;
            tracing::debug!(
                ino,
                rafs_ino,
                file_size = inode.size(),
                offset,
                size,
                read_size,
                chunk_size,
                start_chunk,
                end_chunk,
                "Starting Nydus inode read."
            );
            let mut total = 0_usize;
            let mut remaining = read_size as usize;
            for chunk_idx in start_chunk..=end_chunk {
                self.read_chunk_into_writer(
                    inode.as_ref(),
                    chunk_idx,
                    offset,
                    chunk_size,
                    &mut remaining,
                    &mut total,
                    w,
                )?;
                if remaining == 0 {
                    break;
                }
            }
            Ok(total)
        })();
        let (status, bytes) = match &result {
            Ok(n) => ("ok", *n),
            Err(_) => ("error", 0),
        };
        if let Err(err) = &result {
            error!(
                err = debug(err),
                ino, offset, size, "Failed to read Nydus inode through FUSE."
            );
        }
        self.metrics
            .record_read(status, begin.elapsed().as_secs_f64() * 1000.0, bytes);
        result
    }

    fn release(
        &self,
        _ctx: &Context,
        _inode: Self::Inode,
        _flags: u32,
        _handle: Self::Handle,
        _flush: bool,
        _flock_release: bool,
        _lock_owner: Option<u64>,
    ) -> io::Result<()> {
        Ok(())
    }

    fn statfs(&self, _ctx: &Context, _inode: Self::Inode) -> io::Result<statvfs64> {
        let mut st: statvfs64 = unsafe { std::mem::zeroed() };
        st.f_namemax = 255;
        st.f_bsize = 512;
        st.f_fsid = self.sb.meta.magic as u64;
        #[cfg(target_os = "macos")]
        {
            st.f_files = self.sb.meta.inodes_count as u32;
        }
        #[cfg(target_os = "linux")]
        {
            st.f_files = self.sb.meta.inodes_count;
        }
        Ok(st)
    }

    fn getxattr(
        &self,
        _ctx: &Context,
        inode: Self::Inode,
        name: &CStr,
        size: u32,
    ) -> io::Result<GetxattrReply> {
        if !self.xattr_supported() {
            return Err(io::Error::from_raw_os_error(libc::ENOSYS));
        }
        let name = OsStr::from_bytes(name.to_bytes());
        let inode = self.sb.get_inode(self.to_rafs_ino(inode), false)?;
        let value = inode.get_xattr(name)?;
        match value {
            Some(value) => match size {
                0 => Ok(GetxattrReply::Count((value.len() + 1) as u32)),
                x if x < value.len() as u32 => Err(io::Error::from_raw_os_error(libc::ERANGE)),
                _ => Ok(GetxattrReply::Value(value)),
            },
            None => Err(io::Error::from_raw_os_error(libc::ENODATA)),
        }
    }

    fn listxattr(
        &self,
        _ctx: &Context,
        inode: Self::Inode,
        size: u32,
    ) -> io::Result<ListxattrReply> {
        if !self.xattr_supported() {
            return Err(io::Error::from_raw_os_error(libc::ENOSYS));
        }
        let inode = self.sb.get_inode(self.to_rafs_ino(inode), false)?;
        let mut count = 0;
        let mut buf = Vec::new();
        for mut name in inode.get_xattrs()? {
            count += name.len() + 1;
            if size != 0 {
                buf.append(&mut name);
                buf.push(0);
            }
        }
        match size {
            0 => Ok(ListxattrReply::Count(count as u32)),
            x if x < count as u32 => Err(io::Error::from_raw_os_error(libc::ERANGE)),
            _ => Ok(ListxattrReply::Names(buf)),
        }
    }

    fn readdir(
        &self,
        _ctx: &Context,
        inode: Self::Inode,
        _handle: Self::Handle,
        size: u32,
        offset: u64,
        add_entry: &mut dyn FnMut(DirEntry) -> io::Result<usize>,
    ) -> io::Result<()> {
        self.do_readdir(inode, size, offset, add_entry)
    }

    fn readdirplus(
        &self,
        _ctx: &Context,
        inode: Self::Inode,
        _handle: Self::Handle,
        size: u32,
        offset: u64,
        add_entry: &mut dyn FnMut(DirEntry, Entry) -> io::Result<usize>,
    ) -> io::Result<()> {
        self.do_readdir(inode, size, offset, &mut |dir_entry| {
            let inode = self.sb.get_inode(self.to_rafs_ino(dir_entry.ino), false)?;
            add_entry(dir_entry, self.get_inode_entry(inode.as_ref()))
        })
    }
}
