use crate::backend::chunkdb::CheckSum;
use std::io;

use super::DedupReader;

pub(crate) trait DataTransfer {
    fn copy_from_backend(&mut self, off: usize, pos: usize, len: usize) -> io::Result<usize>;
    fn copy_from_chunk(&mut self, data: &[u8], pos: usize, len: usize) -> io::Result<usize>;
}

pub(crate) struct FetchOp<'a> {
    pub(crate) dedup: &'a DedupReader,
    pub(crate) data: &'a mut [u8],
}

impl<'a> DataTransfer for FetchOp<'a> {
    fn copy_from_backend(&mut self, off: usize, pos: usize, len: usize) -> io::Result<usize> {
        self.dedup.core.b.fetch(off, &mut self.data[pos..pos + len])
    }

    fn copy_from_chunk(&mut self, data: &[u8], pos: usize, len: usize) -> io::Result<usize> {
        self.data[pos..pos + len].copy_from_slice(data);
        Ok(len)
    }
}

#[cfg(target_os = "linux")]
pub(crate) struct FuseWriteOp<'a> {
    pub(crate) reader: &'a DedupReader,
    pub(crate) writer: &'a mut dyn fuse_backend_rs::api::filesystem::ZeroCopyWriter,
}

#[cfg(target_os = "linux")]
impl<'a> DataTransfer for FuseWriteOp<'a> {
    fn copy_from_backend(&mut self, off: usize, _pos: usize, len: usize) -> io::Result<usize> {
        self.reader
            .core
            .b
            .write_to_fuse_writer(off, len as u32, self.writer)
    }

    fn copy_from_chunk(&mut self, data: &[u8], _pos: usize, _len: usize) -> io::Result<usize> {
        self.writer.write(data)
    }
}

pub(crate) fn read_prefetched_chunk<T: DataTransfer>(
    reader: &DedupReader,
    cs: &CheckSum,
    start: usize,
    len: usize,
    op: &mut T,
    pos: usize,
) -> io::Result<Option<usize>> {
    match reader
        .core
        .chunk_db
        .with_chunk_range(cs, start, len, |slice| op.copy_from_chunk(slice, pos, len))
    {
        Ok(Some(n)) => Ok(Some(n)),
        Ok(None) => {
            if let Some(client) = &reader.core.local_chunk_client {
                let _ = client.prefetch_chunk_blocking(cs);
                match reader
                    .core
                    .chunk_db
                    .with_chunk_range(cs, start, len, |slice| op.copy_from_chunk(slice, pos, len))
                {
                    Ok(Some(n)) => Ok(Some(n)),
                    Ok(None) => Ok(None),
                    Err(e) => Err(io::Error::other(e.to_string())),
                }
            } else {
                Ok(None)
            }
        }
        Err(e) => Err(io::Error::other(e.to_string())),
    }
}
