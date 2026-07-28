use std::fs::File;
use std::os::fd::AsRawFd;
use std::sync::atomic::AtomicPtr;
use std::sync::atomic::Ordering::Relaxed;

#[derive(Debug)]
pub(crate) struct MmapInner {
    pub(crate) file: File,
    buf: AtomicPtr<u8>,
    len: usize,
}

impl MmapInner {
    pub(crate) fn new(file: File) -> std::io::Result<Self> {
        let meta = file.metadata()?;
        let buf = unsafe {
            let ptr = libc::mmap(
                std::ptr::null_mut(),
                meta.len() as libc::size_t,
                libc::PROT_READ | libc::PROT_WRITE,
                libc::MAP_SHARED,
                file.as_raw_fd(),
                0,
            );
            if ptr == libc::MAP_FAILED {
                Err(std::io::Error::last_os_error())
            } else {
                Ok(ptr as *mut u8)
            }
        }?;
        Ok(Self {
            file,
            buf: AtomicPtr::new(buf),
            len: meta.len() as usize,
        })
    }

    fn base(&self) -> *mut u8 {
        self.buf.load(Relaxed)
    }

    #[allow(clippy::mut_from_ref)]
    pub(crate) fn as_mut_slice(&self) -> &mut [u8] {
        unsafe { std::slice::from_raw_parts_mut(self.base(), self.len) }
    }

    pub(crate) fn as_slice(&self) -> &[u8] {
        unsafe { std::slice::from_raw_parts(self.base() as *const u8, self.len) }
    }
}

impl Drop for MmapInner {
    fn drop(&mut self) {
        unsafe {
            libc::munmap(self.base() as *mut libc::c_void, self.len);
        }
    }
}
