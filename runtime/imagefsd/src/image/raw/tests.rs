use super::*;
#[cfg(target_os = "linux")]
use crate::backend::CHUNK_SIZE;
#[cfg(target_os = "linux")]
use crate::test_metrics::{histogram_points_f64, sum_points_u64, MetricsHarness};
#[cfg(target_os = "linux")]
use fuse_backend_rs::api::filesystem::{Context, FileSystem, ZeroCopyWriter};
#[cfg(target_os = "linux")]
use std::collections::BTreeMap;
use std::io::{Error, ErrorKind};
#[cfg(target_os = "linux")]
use std::io::{Result as IoResult, Write};
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Debug)]
struct MockSuccessBackend {
    content: Vec<u8>,
}

impl Backend for MockSuccessBackend {
    fn size(&self) -> u64 {
        self.content.len() as u64
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        let end = off + data.len();
        if off >= self.content.len() {
            return Ok(0);
        }
        let src = &self.content[off..std::cmp::min(end, self.content.len())];
        let len = src.len();
        data[..len].copy_from_slice(src);
        Ok(len)
    }

    fn write_to_fuse_writer(
        &self,
        _off: usize,
        _size: u32,
        _w: &mut dyn ZeroCopyWriter,
    ) -> std::io::Result<usize> {
        unimplemented!("write_to_fuse_writer is not tested")
    }
}

impl BackendEx for MockSuccessBackend {
    fn invalidate_chunk(&self, _chunk_id: usize) -> std::io::Result<()> {
        Ok(())
    }
}

#[derive(Debug)]
struct MockFailureBackend;

impl Backend for MockFailureBackend {
    fn size(&self) -> u64 {
        0
    }

    fn fetch(&self, _off: usize, _data: &mut [u8]) -> std::io::Result<usize> {
        Err(Error::new(
            ErrorKind::PermissionDenied,
            "mock access denied",
        ))
    }

    fn write_to_fuse_writer(
        &self,
        _off: usize,
        _size: u32,
        _w: &mut dyn ZeroCopyWriter,
    ) -> std::io::Result<usize> {
        unimplemented!("write_to_fuse_writer is not tested")
    }
}

impl BackendEx for MockFailureBackend {
    fn invalidate_chunk(&self, _chunk_id: usize) -> std::io::Result<()> {
        Ok(())
    }
}

#[test]
fn test_raw_image_new() {
    let backend = Arc::new(MockSuccessBackend { content: vec![] });
    let name = "test_image";
    let before = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs();

    let image = RawImage::new(name, backend, None, None).unwrap();

    assert_eq!(image.name, name);
    assert_eq!(image.size, 0);
    assert!(image.create_tm >= before, "Creation time should be recent");
}

#[test]
fn test_backend_trait_size() {
    let expected_size = 4096;
    let backend = Arc::new(MockSuccessBackend {
        content: vec![0u8; expected_size],
    });
    let image = RawImage::new("size_test", backend, None, None).unwrap();

    assert_eq!(image.size(), expected_size as u64);
}

#[test]
fn test_fetch_delegates_to_successful_backend() {
    let mock_content = vec![10, 20, 30, 40, 50];
    let backend = Arc::new(MockSuccessBackend {
        content: mock_content.clone(),
    });
    let image = RawImage::new("success_fetch", backend, None, None).unwrap();
    let mut buffer = [0u8; 5];

    let result = image.fetch(0, &mut buffer);

    assert!(result.is_ok());
    assert_eq!(result.unwrap(), 5);
    assert_eq!(buffer, mock_content.as_slice());
}

#[test]
fn test_fetch_delegates_to_failing_backend() {
    let backend = Arc::new(MockFailureBackend);
    let image = RawImage::new("failure_fetch", backend, None, None).unwrap();
    let mut buffer = [0u8; 10];

    let result = image.fetch(0, &mut buffer);

    assert!(result.is_err());
    let err = result.unwrap_err();
    assert_eq!(err.kind(), ErrorKind::PermissionDenied);
    assert_eq!(err.to_string(), "mock access denied");
}

#[cfg(target_os = "linux")]
#[derive(Debug)]
struct MockBackendEx {
    data: Vec<u8>,
}

#[cfg(target_os = "linux")]
impl MockBackendEx {
    fn new(size: usize) -> Self {
        let data = (0..size).map(|i| (i % 256) as u8).collect();
        Self { data }
    }
}

#[cfg(target_os = "linux")]
impl Backend for MockBackendEx {
    fn size(&self) -> u64 {
        self.data.len() as u64
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        if off >= self.data.len() {
            return Ok(0);
        }
        let available = self.data.len() - off;
        let len_to_read = std::cmp::min(data.len(), available);
        data[..len_to_read].copy_from_slice(&self.data[off..off + len_to_read]);
        Ok(len_to_read)
    }

    fn write_to_fuse_writer(
        &self,
        _off: usize,
        _size: u32,
        _w: &mut dyn ZeroCopyWriter,
    ) -> std::io::Result<usize> {
        unimplemented!();
    }
}

#[cfg(target_os = "linux")]
impl BackendEx for MockBackendEx {
    fn invalidate_chunk(&self, _chunk_id: usize) -> std::io::Result<()> {
        Ok(())
    }
}

#[cfg(target_os = "linux")]
#[derive(Debug)]
struct MockReadBackend {
    data: Vec<u8>,
}

#[cfg(target_os = "linux")]
impl Backend for MockReadBackend {
    fn size(&self) -> u64 {
        self.data.len() as u64
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        if off >= self.data.len() {
            return Ok(0);
        }
        let end = (off + data.len()).min(self.data.len());
        let len = end - off;
        data[..len].copy_from_slice(&self.data[off..end]);
        Ok(len)
    }

    fn write_to_fuse_writer(
        &self,
        off: usize,
        size: u32,
        w: &mut dyn ZeroCopyWriter,
    ) -> std::io::Result<usize> {
        let end = (off + size as usize).min(self.data.len());
        w.write(&self.data[off..end])
    }
}

#[cfg(target_os = "linux")]
impl BackendEx for MockReadBackend {
    fn invalidate_chunk(&self, _chunk_id: usize) -> std::io::Result<()> {
        Ok(())
    }
}

#[cfg(target_os = "linux")]
#[derive(Default)]
struct VecWriter {
    data: Vec<u8>,
}

#[cfg(target_os = "linux")]
impl Write for VecWriter {
    fn write(&mut self, buf: &[u8]) -> IoResult<usize> {
        self.data.extend_from_slice(buf);
        Ok(buf.len())
    }

    fn flush(&mut self) -> IoResult<()> {
        Ok(())
    }
}

#[cfg(target_os = "linux")]
impl ZeroCopyWriter for VecWriter {
    fn write_from(
        &mut self,
        _f: &mut dyn fuse_backend_rs::common::file_traits::FileReadWriteVolatile,
        _count: usize,
        _off: u64,
    ) -> IoResult<usize> {
        Err(Error::new(
            ErrorKind::Unsupported,
            "write_from is not used in this test",
        ))
    }

    fn available_bytes(&self) -> usize {
        usize::MAX
    }
}

#[cfg(target_os = "linux")]
fn attrs(pairs: &[(&str, &str)]) -> BTreeMap<String, String> {
    pairs
        .iter()
        .map(|(key, value)| ((*key).to_string(), (*value).to_string()))
        .collect()
}

#[cfg(target_os = "linux")]
fn setup_test_env(image_size: usize) -> (RawDedupImage, Arc<MockBackendEx>) {
    let dir = tempfile::tempdir().unwrap();
    let g_path = dir.path().join("global");
    let l_path = dir.path().join("local");
    std::fs::create_dir(&g_path).unwrap();
    std::fs::create_dir(&l_path).unwrap();

    let mock_backend = Arc::new(MockBackendEx::new(image_size));
    let chunk_db = Arc::new(ChunkDB::new(&g_path).unwrap());
    let index_db = Arc::new(IndexDB::open(&l_path).unwrap());
    let dedup =
        DedupReader::new(mock_backend.clone(), chunk_db, index_db, "test-image", None).unwrap();

    (RawDedupImage::new(dedup).unwrap(), mock_backend)
}

#[cfg(target_os = "linux")]
#[test]
fn test_validate_range() {
    let image_size = 5 * CHUNK_SIZE;
    let (raw_image, _) = setup_test_env(image_size);

    let (off, size) = raw_image.validate_range(CHUNK_SIZE, CHUNK_SIZE).unwrap();
    assert_eq!(off, CHUNK_SIZE);
    assert_eq!(size, CHUNK_SIZE);

    let (off, size) = raw_image
        .validate_range(4 * CHUNK_SIZE, 2 * CHUNK_SIZE)
        .unwrap();
    assert_eq!(off, 4 * CHUNK_SIZE);
    assert_eq!(size, CHUNK_SIZE);

    let res = raw_image.validate_range(image_size + 1, CHUNK_SIZE);
    assert!(res.is_err());
    assert_eq!(res.err().unwrap().kind(), ErrorKind::InvalidInput);

    let (off, size) = raw_image.validate_range(image_size, 0).unwrap();
    assert_eq!(off, image_size);
    assert_eq!(size, 0);
}

#[cfg(target_os = "linux")]
#[test]
fn test_fetch_correctness() {
    let image_size = 10 * CHUNK_SIZE;
    let (raw_image, backend) = setup_test_env(image_size);

    let off = 2 * CHUNK_SIZE + 100;
    let len = 2 * CHUNK_SIZE;
    let mut buffer = vec![0u8; len];

    let bytes_read = raw_image.fetch(off, &mut buffer).unwrap();
    assert_eq!(bytes_read, len);

    let mut expected_data = vec![0u8; len];
    backend.fetch(off, &mut expected_data).unwrap();
    assert_eq!(buffer, expected_data);
}

#[cfg(target_os = "linux")]
#[test]
fn test_fetch_on_same_region_twice() {
    let image_size = 5 * CHUNK_SIZE;
    let (raw_image, backend) = setup_test_env(image_size);

    let off = CHUNK_SIZE;
    let mut buffer = vec![0u8; CHUNK_SIZE];

    raw_image.fetch(off, &mut buffer).unwrap();
    let mut expected = vec![0u8; CHUNK_SIZE];
    backend.fetch(off, &mut expected).unwrap();
    assert_eq!(buffer, expected);

    let mut buffer2 = vec![0u8; CHUNK_SIZE];
    raw_image.fetch(off, &mut buffer2).unwrap();
    assert_eq!(buffer2, expected);
}

#[cfg(target_os = "linux")]
#[test]
fn test_raw_image_read_emits_fs_metrics() {
    let harness = MetricsHarness::new();
    let data = (0..128_u8).collect::<Vec<_>>();
    let backend = Arc::new(MockReadBackend { data: data.clone() });
    let image = RawImage {
        name: "raw-metrics".to_string(),
        size: data.len() as u64,
        create_tm: 0,
        b: backend,
        metrics: FsReadMetrics::with_meter(&harness.meter("imagefsd.fs.test"), "raw"),
    };
    let mut writer = VecWriter::default();

    let read = image
        .read(
            &Context::new(),
            IMAGE_FILE_ID,
            0,
            &mut writer,
            32,
            8,
            None,
            0,
        )
        .unwrap();

    assert_eq!(read, 32);
    assert_eq!(writer.data, data[8..40].to_vec());

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.fs.read_total")
        .contains(&(attrs(&[("image_type", "raw"), ("result", "ok")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.fs.read_bytes")
        .contains(&(attrs(&[("image_type", "raw"), ("result", "ok")]), 32)));
    assert!(
        histogram_points_f64(&collected, "imagefsd.fs.read_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs == &attrs(&[("image_type", "raw"), ("result", "ok")]) && *count == 1
            })
    );
}

#[cfg(target_os = "linux")]
#[test]
fn test_fetch_empty_buffer() {
    let image_size = 2 * CHUNK_SIZE;
    let (raw_image, _backend) = setup_test_env(image_size);

    let mut buffer = vec![];
    let bytes_read = raw_image.fetch(0, &mut buffer).unwrap();
    assert_eq!(bytes_read, 0);
}
