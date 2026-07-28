mod inflight;
mod metrics;
mod mmap;

use crate::backend::{Backend, BackendEx, CHUNK_SIZE};
use crate::utils::{align_up, new_std_io_error, page_size};
use fuse_backend_rs::api::filesystem::ZeroCopyWriter;
use opentelemetry::global;
use std::collections::BTreeMap;
use std::fmt::Formatter;
use std::fs::File;
#[cfg(target_os = "linux")]
use std::os::fd::AsRawFd;
#[cfg(not(target_os = "linux"))]
use std::os::unix::fs::FileExt;
#[cfg(test)]
use std::sync::atomic::Ordering::Relaxed;
use std::sync::atomic::Ordering::{Acquire, SeqCst};
use std::sync::atomic::{AtomicBool, AtomicU8};
use std::sync::{Arc, Mutex, RwLock};
use std::time::Instant;
use tracing::{error, info};

use self::inflight::{InFlightIO, ProcessIOGuard};
use self::metrics::CacheMetrics;
use self::mmap::MmapInner;

#[derive(Debug)]
pub enum CacheError {
    IoError(std::io::Error),
    InvalidRange,
    Timeout,
}

impl std::fmt::Display for CacheError {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            CacheError::IoError(e) => {
                write!(f, "io_error: {}", e)
            }
            CacheError::InvalidRange => {
                write!(f, "invalid range")
            }
            CacheError::Timeout => {
                write!(f, "timeout")
            }
        }
    }
}

impl From<std::io::Error> for CacheError {
    fn from(e: std::io::Error) -> Self {
        Self::IoError(e)
    }
}

impl std::error::Error for CacheError {}

pub type CacheResult<T> = Result<T, CacheError>;

#[derive(Debug, Default, Eq, PartialEq)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub(crate) struct ReadaheadOutcome {
    pub bytes: usize,
    pub cached_chunks: usize,
    pub skipped_chunks: usize,
}

#[cfg(target_os = "linux")]
fn punch_hole(file: &File, start: usize, size: usize) -> std::io::Result<()> {
    let fd = file.as_raw_fd();
    let mode = libc::FALLOC_FL_PUNCH_HOLE | libc::FALLOC_FL_KEEP_SIZE;
    let ret = unsafe { libc::fallocate(fd, mode, start as libc::off_t, size as libc::off_t) };
    if ret == -1 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(())
}

#[cfg(not(target_os = "linux"))]
fn punch_hole(file: &File, start: usize, size: usize) -> std::io::Result<()> {
    let zeros = vec![0_u8; size.min(CHUNK_SIZE)];
    let mut written = 0;
    while written < size {
        let chunk = (size - written).min(zeros.len());
        let n = file.write_at(&zeros[..chunk], (start + written) as u64)?;
        if n == 0 {
            return Err(std::io::Error::new(
                std::io::ErrorKind::WriteZero,
                "failed to zero cached range",
            ));
        }
        written += n;
    }
    Ok(())
}

fn bitmap_file_size(size: u64) -> u64 {
    let nr_chunks = align_up(size, CHUNK_SIZE as u64) / CHUNK_SIZE as u64;
    (align_up(nr_chunks, 8) / 8).max(page_size())
}

fn set_bit(target: &AtomicU8, bit_shift: usize) {
    loop {
        let old = target.load(Acquire);
        let new = old | (1_u8 << bit_shift);
        if old == new {
            break;
        }
        if target.compare_exchange(old, new, SeqCst, SeqCst).is_ok() {
            break;
        }
    }
}

fn clear_bit(target: &AtomicU8, bit_shift: usize) {
    loop {
        let old = target.load(Acquire);
        let new = old & !(1_u8 << bit_shift);
        if old == new {
            break;
        }
        if target.compare_exchange(old, new, SeqCst, SeqCst).is_ok() {
            break;
        }
    }
}

#[derive(Debug)]
pub struct EmptyBackend(u64);

impl Backend for EmptyBackend {
    fn size(&self) -> u64 {
        self.0
    }

    fn fetch(&self, _off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        Ok(data.len())
    }
}

#[derive(Debug)]
pub struct Cache<B> {
    raw: MmapInner,
    bitmap: MmapInner,
    drop_lock: RwLock<()>,
    in_flight_ios: Mutex<BTreeMap<usize, Arc<InFlightIO>>>,
    metrics: CacheMetrics,
    b: B,
}

impl Cache<EmptyBackend> {
    pub(crate) fn from_raw_file(
        file_path: &str,
        node_id: &str,
    ) -> CacheResult<Cache<EmptyBackend>> {
        let file = std::fs::OpenOptions::new()
            .write(true)
            .read(true)
            .open(file_path)?;
        let bitmap_path = format!("{}.bitmap", file_path);
        let meta = file.metadata()?;
        let bitmap_file = std::fs::OpenOptions::new()
            .write(true)
            .read(true)
            .create(true)
            .truncate(false)
            .open(bitmap_path)?;
        bitmap_file.set_len(bitmap_file_size(meta.len()))?;
        let c = Self::raw(file, bitmap_file, EmptyBackend(meta.len()), node_id)?;
        c.fetch_data(0, meta.len() as usize)?;
        Ok(c)
    }
}

impl<B: Backend> Cache<B> {
    fn raw(raw_file: File, bitmap_file: File, b: B, node_id: &str) -> CacheResult<Self> {
        let meter = global::meter("imagefsd.cache");
        Ok(Self {
            raw: MmapInner::new(raw_file)?,
            bitmap: MmapInner::new(bitmap_file)?,
            drop_lock: Default::default(),
            in_flight_ios: Mutex::new(BTreeMap::new()),
            metrics: CacheMetrics::new(&meter, node_id),
            b,
        })
    }

    #[cfg(test)]
    pub(crate) fn new(b: B, cache_file_path: &str) -> CacheResult<Self> {
        Self::new_with_node_id(b, cache_file_path, "")
    }

    pub(crate) fn new_with_node_id(
        b: B,
        cache_file_path: &str,
        node_id: &str,
    ) -> CacheResult<Self> {
        let bitmap_path = format!("{}.bitmap", cache_file_path);
        let raw_file = std::fs::OpenOptions::new()
            .write(true)
            .read(true)
            .create(true)
            .truncate(false)
            .open(cache_file_path)?;
        let meta = raw_file.metadata()?;
        if meta.len() != b.size() {
            raw_file.set_len(b.size())?;
        }
        let bitmap_file = std::fs::OpenOptions::new()
            .write(true)
            .read(true)
            .create(true)
            .truncate(false)
            .open(bitmap_path)?;
        bitmap_file.set_len(bitmap_file_size(b.size()))?;
        Self::raw(raw_file, bitmap_file, b, node_id)
    }

    fn register_or_wait_inflight_io(&self, chunk_idx: usize) -> (Arc<InFlightIO>, bool) {
        let mut in_flight_ios = self.in_flight_ios.lock().unwrap();
        if let Some(io) = in_flight_ios.get(&chunk_idx) {
            (io.clone(), true)
        } else {
            let io = Arc::new(InFlightIO::new());
            in_flight_ios.insert(chunk_idx, io.clone());
            (io, false)
        }
    }

    fn remove_inflight_io(&self, chunk_idx: usize) {
        self.in_flight_ios.lock().unwrap().remove(&chunk_idx);
    }

    fn fetch_backend_exact(
        &self,
        path: &'static str,
        offset: usize,
        data: &mut [u8],
    ) -> CacheResult<usize> {
        let started_at = Instant::now();
        let mut fetched = 0;
        while fetched < data.len() {
            match self.b.fetch(offset + fetched, &mut data[fetched..]) {
                Ok(0) => {
                    self.metrics.record_backend_fetch(
                        path,
                        "short_read",
                        started_at.elapsed().as_secs_f64() * 1000.0,
                        0,
                    );
                    return Err(CacheError::IoError(std::io::Error::new(
                        std::io::ErrorKind::UnexpectedEof,
                        format!(
                            "short backend cache read: got {fetched}, want {}",
                            data.len()
                        ),
                    )));
                }
                Ok(read) if read <= data.len() - fetched => fetched += read,
                Ok(read) => {
                    self.metrics.record_backend_fetch(
                        path,
                        "invalid_read",
                        started_at.elapsed().as_secs_f64() * 1000.0,
                        0,
                    );
                    return Err(CacheError::IoError(std::io::Error::new(
                        std::io::ErrorKind::InvalidData,
                        format!(
                            "backend cache read returned {read} bytes for a {} byte buffer",
                            data.len() - fetched
                        ),
                    )));
                }
                Err(error) => {
                    self.metrics.record_backend_fetch(
                        path,
                        "io_error",
                        started_at.elapsed().as_secs_f64() * 1000.0,
                        0,
                    );
                    return Err(error.into());
                }
            }
        }
        self.metrics.record_backend_fetch(
            path,
            "ok",
            started_at.elapsed().as_secs_f64() * 1000.0,
            fetched,
        );
        Ok(fetched)
    }

    fn fetch_data(&self, off: usize, len: usize) -> CacheResult<()> {
        if len == 0 {
            return Ok(());
        }
        let first_chunk_idx = off / CHUNK_SIZE;
        let last_chunk_idx = (off + len - 1) / CHUNK_SIZE;
        let bitmap_data = self.bitmap.as_mut_slice();
        let raw_data = self.raw.as_mut_slice();
        'next_chunk: for idx in first_chunk_idx..=last_chunk_idx {
            let bit_at = idx / 8;
            let bit_shift = idx % 8;
            let raw_ptr: *mut u8 = &mut bitmap_data[bit_at] as *mut u8;
            let atomic_ref: &AtomicU8 = unsafe { &*(raw_ptr as *const AtomicU8) };
            loop {
                if atomic_ref.load(Acquire) & (1_u8 << bit_shift) != 0 {
                    continue 'next_chunk;
                }
                let (io, do_wait) = self.register_or_wait_inflight_io(idx);
                if do_wait {
                    let started_at = Instant::now();
                    let timeout = io.wait();
                    self.metrics.record_inflight_wait(
                        if timeout { "timeout" } else { "notified" },
                        started_at.elapsed().as_secs_f64() * 1000.0,
                    );
                    if timeout {
                        error!(chunk_idx = idx, "Wait fetch data timeout");
                        return Err(CacheError::Timeout);
                    }
                    continue;
                }
                let _guard = ProcessIOGuard(io);
                let start = idx * CHUNK_SIZE;
                let end = if (idx + 1) * CHUNK_SIZE <= self.b.size() as usize {
                    (idx + 1) * CHUNK_SIZE
                } else {
                    self.b.size() as usize
                };
                if let Err(error) =
                    self.fetch_backend_exact("foreground", start, &mut raw_data[start..end])
                {
                    self.remove_inflight_io(idx);
                    return Err(error);
                }
                set_bit(atomic_ref, bit_shift);
                self.remove_inflight_io(idx);
                break;
            }
        }
        Ok(())
    }

    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    pub(crate) fn readahead(
        &self,
        off: usize,
        len: usize,
        cancelled: &AtomicBool,
    ) -> CacheResult<ReadaheadOutcome> {
        let started_at = Instant::now();
        let result = self.readahead_inner(off, len, cancelled);
        let (status, bytes, cached_chunks, skipped_chunks) = match &result {
            Ok(outcome) if cancelled.load(Acquire) => (
                "cancelled",
                outcome.bytes,
                outcome.cached_chunks,
                outcome.skipped_chunks,
            ),
            Ok(outcome) => (
                "ok",
                outcome.bytes,
                outcome.cached_chunks,
                outcome.skipped_chunks,
            ),
            Err(CacheError::Timeout) => ("timeout", 0, 0, 0),
            Err(CacheError::InvalidRange) => ("invalid_range", 0, 0, 0),
            Err(CacheError::IoError(err)) if err.kind() == std::io::ErrorKind::Unsupported => {
                ("unsupported", 0, 0, 0)
            }
            Err(CacheError::IoError(_)) => ("io_error", 0, 0, 0),
        };
        self.metrics.record_readahead(
            status,
            started_at.elapsed().as_secs_f64(),
            bytes,
            cached_chunks,
            skipped_chunks,
        );
        result
    }

    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    fn readahead_inner(
        &self,
        off: usize,
        len: usize,
        cancelled: &AtomicBool,
    ) -> CacheResult<ReadaheadOutcome> {
        let _drop_guard = self.drop_lock.read().unwrap();
        if cancelled.load(Acquire) || len == 0 || off >= self.b.size() as usize {
            return Ok(ReadaheadOutcome::default());
        }
        let end_offset = off.saturating_add(len).min(self.b.size() as usize);
        let first_chunk = off / CHUNK_SIZE;
        let last_chunk = (end_offset - 1) / CHUNK_SIZE;
        let mut outcome = ReadaheadOutcome::default();
        let mut buffer = vec![0_u8; CHUNK_SIZE];
        let bitmap_data = self.bitmap.as_mut_slice();
        let raw_data = self.raw.as_mut_slice();

        for idx in first_chunk..=last_chunk {
            if cancelled.load(Acquire) {
                break;
            }
            let start = idx * CHUNK_SIZE;
            let end = ((idx + 1) * CHUNK_SIZE).min(self.b.size() as usize);
            let len = end - start;
            let bit_at = idx / 8;
            let bit_shift = idx % 8;
            let raw_ptr: *mut u8 = &mut bitmap_data[bit_at] as *mut u8;
            let atomic_ref: &AtomicU8 = unsafe { &*(raw_ptr as *const AtomicU8) };
            if atomic_ref.load(Acquire) & (1_u8 << bit_shift) != 0 {
                outcome.skipped_chunks += 1;
                continue;
            }

            let fetched = self.fetch_backend_exact("readahead", start, &mut buffer[..len])?;
            outcome.bytes += fetched;

            // Remote I/O happens before claiming the foreground in-flight slot.
            // A foreground miss can therefore proceed independently; this short
            // critical section only serializes the final cache commit.
            let (io, do_wait) = self.register_or_wait_inflight_io(idx);
            if do_wait {
                outcome.skipped_chunks += 1;
                continue;
            }
            let _io_guard = ProcessIOGuard(io);
            if atomic_ref.load(Acquire) & (1_u8 << bit_shift) == 0 {
                raw_data[start..end].copy_from_slice(&buffer[..len]);
                set_bit(atomic_ref, bit_shift);
                outcome.cached_chunks += 1;
            } else {
                outcome.skipped_chunks += 1;
            }
            self.remove_inflight_io(idx);
        }
        Ok(outcome)
    }

    fn fix_range(&self, off: usize, size: usize) -> CacheResult<(usize, usize)> {
        if off >= self.b.size() as usize {
            return Err(CacheError::InvalidRange);
        }
        let end = if off + size > self.b.size() as usize {
            self.b.size() as usize
        } else {
            off + size
        };
        Ok((off, end))
    }

    pub fn read_at(&self, off: usize, buf: &mut [u8]) -> CacheResult<usize> {
        let begin = Instant::now();
        if buf.is_empty() {
            self.metrics.record_read("ok", 0.0, 0);
            return Ok(0);
        }
        let _guard = self.drop_lock.read().unwrap();
        let result = (|| {
            let (off, end) = self.fix_range(off, buf.len())?;
            let len = end - off;
            self.fetch_data(off, len)?;
            let raw_data = self.raw.as_slice();
            buf[..len].copy_from_slice(&raw_data[off..end]);
            Ok(len)
        })();
        let (status, bytes) = match &result {
            Ok(len) => ("ok", *len),
            Err(CacheError::InvalidRange) => ("invalid_range", 0),
            Err(CacheError::Timeout) => ("timeout", 0),
            Err(CacheError::IoError(_)) => ("io_error", 0),
        };
        self.metrics
            .record_read(status, begin.elapsed().as_secs_f64() * 1000.0, bytes);
        result
    }
}

impl<B: Backend> Backend for Cache<B> {
    fn size(&self) -> u64 {
        self.b.size()
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        self.read_at(off, data).map_err(new_std_io_error)
    }

    fn write_to_fuse_writer(
        &self,
        off: usize,
        size: u32,
        w: &mut dyn ZeroCopyWriter,
    ) -> std::io::Result<usize> {
        let (off, end) = self
            .fix_range(off, size as usize)
            .map_err(new_std_io_error)?;
        let _guard = self.drop_lock.read().unwrap();
        self.fetch_data(off, end - off).map_err(new_std_io_error)?;
        let data = self.raw.as_slice();
        w.write(&data[off..end])
    }
}

impl<B: Backend> BackendEx for Cache<B> {
    fn invalidate_chunk(&self, chunk_id: usize) -> std::io::Result<()> {
        let _guard = self.drop_lock.write().unwrap();
        if (chunk_id * CHUNK_SIZE) >= self.size() as usize {
            return Ok(());
        }
        let bitmap_data = self.bitmap.as_mut_slice();
        let bit_at = chunk_id / 8;
        let bit_shift = chunk_id % 8;
        let raw_ptr: *mut u8 = &mut bitmap_data[bit_at] as *mut u8;
        let atomic_ref: &AtomicU8 = unsafe { &*(raw_ptr as *const AtomicU8) };
        if (atomic_ref.load(Acquire) & (1_u8 << bit_shift)) == 0 {
            return Ok(());
        }
        clear_bit(atomic_ref, bit_shift);
        self.bitmap.file.sync_data()?;
        let start = chunk_id * CHUNK_SIZE;
        let size = CHUNK_SIZE;
        info!(offset = start, length = size, "Invalidate cache.");
        punch_hole(&self.raw.file, start, size)
    }
}

#[cfg(test)]
mod tests;
