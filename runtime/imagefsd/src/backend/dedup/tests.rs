use super::*;
use crate::backend::indexdb::{DedupRange, IndexDB};
use crate::backend::peer::{
    ChunkIndex, ChunkServer, LocalChunkClient, PeerClient, PeerRuntime, StaticPeers,
};
use crate::backend::{Backend, BackendEx, CHUNK_SIZE};
use crate::test_metrics::{histogram_points_f64, sum_points_u64, MetricsHarness};
use async_trait::async_trait;
use rand::prelude::*;
use std::collections::BTreeMap;
use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};
use tempfile::TempDir;

#[derive(Debug, Clone)]
struct MockBackend {
    state: Arc<Mutex<MockBackendState>>,
}

#[derive(Debug)]
struct MockBackendState {
    data: HashMap<usize, u8>,
    size: u64,
    fetch_calls: u64,
    invalidate_calls: Vec<usize>,
}

impl MockBackend {
    fn new(size: u64) -> Self {
        let mut data = HashMap::new();
        let mut rng = thread_rng();
        for i in 0..size {
            data.insert(i as usize, rng.gen());
        }

        Self {
            state: Arc::new(Mutex::new(MockBackendState {
                data,
                size,
                fetch_calls: 0,
                invalidate_calls: vec![],
            })),
        }
    }
}

impl Backend for MockBackend {
    fn size(&self) -> u64 {
        self.state.lock().unwrap().size
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> io::Result<usize> {
        let mut state = self.state.lock().unwrap();
        state.fetch_calls += 1;

        if off >= state.size as usize {
            return Ok(0);
        }

        let len = data.len().min(state.size as usize - off);
        for i in 0..len {
            data[i] = *state.data.get(&(off + i)).unwrap_or(&0);
        }
        Ok(len)
    }

    fn write_to_fuse_writer(
        &self,
        _off: usize,
        _size: u32,
        _w: &mut dyn ZeroCopyWriter,
    ) -> io::Result<usize> {
        unimplemented!()
    }
}

impl BackendEx for MockBackend {
    fn invalidate_chunk(&self, chunk_id: usize) -> io::Result<()> {
        let mut state = self.state.lock().unwrap();
        state.invalidate_calls.push(chunk_id);
        Ok(())
    }
}

fn setup_test() -> (DedupReader, Arc<MockBackend>, TempDir, TempDir) {
    let g_store_dir = tempfile::tempdir().unwrap();
    let l_store_dir = tempfile::tempdir().unwrap();
    let backend = Arc::new(MockBackend::new(2 * CHUNK_SIZE as u64));
    let chunk_db = Arc::new(ChunkDB::new(g_store_dir.path()).unwrap());
    let index_db = Arc::new(IndexDB::open(l_store_dir.path()).unwrap());
    let dedup = DedupReader::new(backend.clone(), chunk_db, index_db, "test-dedup", None).unwrap();

    (dedup, backend, g_store_dir, l_store_dir)
}

#[derive(Debug, Default)]
struct RecordingChunkIndex {
    registered: Arc<Mutex<Vec<CheckSum>>>,
    unregistered: Arc<Mutex<Vec<CheckSum>>>,
}

impl RecordingChunkIndex {
    fn registered(&self) -> Vec<CheckSum> {
        self.registered.lock().unwrap().clone()
    }
}

#[async_trait]
impl ChunkIndex for RecordingChunkIndex {
    async fn lookup_owners(&self, _cs: &CheckSum) -> anyhow::Result<Vec<SocketAddr>> {
        Ok(Vec::new())
    }

    async fn register(&self, cs: &CheckSum) -> anyhow::Result<()> {
        self.registered.lock().unwrap().push(*cs);
        Ok(())
    }

    async fn register_batch(&self, checksums: &[CheckSum]) -> anyhow::Result<()> {
        self.registered.lock().unwrap().extend_from_slice(checksums);
        Ok(())
    }

    async fn unregister(&self, cs: &CheckSum) -> anyhow::Result<()> {
        self.unregistered.lock().unwrap().push(*cs);
        Ok(())
    }
}

fn setup_test_with_local_chunk_client() -> (
    DedupReader,
    Arc<MockBackend>,
    Arc<RecordingChunkIndex>,
    crate::backend::peer::ShutdownHandle,
    std::thread::JoinHandle<anyhow::Result<()>>,
    TempDir,
    TempDir,
    TempDir,
) {
    let g_store_dir = tempfile::tempdir().unwrap();
    let l_store_dir = tempfile::tempdir().unwrap();
    let server_dir = tempfile::tempdir().unwrap();
    let backend = Arc::new(MockBackend::new(2 * CHUNK_SIZE as u64));
    let index_db = Arc::new(IndexDB::open(l_store_dir.path()).unwrap());
    let runtime = PeerRuntime::new().unwrap();
    let sock = server_dir.path().join("chunkserver.sock");
    let index = Arc::new(RecordingChunkIndex::default());
    let discovery = Arc::new(StaticPeers::new(Vec::new()));
    let peer_client =
        Arc::new(PeerClient::new(runtime.clone(), discovery).with_chunk_index(index.clone()));
    let server = ChunkServer::new(
        runtime.clone(),
        Arc::new(ChunkDB::new(server_dir.path()).unwrap()),
        "127.0.0.1:0".parse().unwrap(),
        &sock,
        Some(peer_client),
    )
    .unwrap();
    let shutdown = server.shutdown_handle();
    let handle = std::thread::spawn(move || server.run());
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(3);
    while !sock.exists() {
        assert!(
            std::time::Instant::now() < deadline,
            "socket was not created in time"
        );
        std::thread::sleep(std::time::Duration::from_millis(20));
    }
    let local_chunk_client = Arc::new(LocalChunkClient::new(
        runtime,
        &sock,
        std::time::Duration::from_millis(300),
    ));
    let chunk_db = Arc::new(
        ChunkDB::new_with_index_ctl(
            g_store_dir.path(),
            Some(local_chunk_client.clone() as Arc<dyn crate::backend::chunkdb::ChunkIndexControl>),
        )
        .unwrap(),
    );
    let dedup = DedupReader::new(
        backend.clone(),
        chunk_db,
        index_db,
        "test-dedup",
        Some(local_chunk_client),
    )
    .unwrap();

    (
        dedup,
        backend,
        index,
        shutdown,
        handle,
        g_store_dir,
        l_store_dir,
        server_dir,
    )
}

fn attrs(pairs: &[(&str, &str)]) -> BTreeMap<String, String> {
    pairs
        .iter()
        .map(|(key, value)| ((*key).to_string(), (*value).to_string()))
        .collect()
}

#[test]
fn test_checksum_logic() {
    let data = b"hello world";
    let cs_sha256 = CheckSum::from_data(data, CheckSumMethod::Sha256);
    let cs_blake3 = CheckSum::from_data(data, CheckSumMethod::Blake3);

    assert_ne!(cs_sha256.method, CheckSumMethod::Unknown);
    assert_ne!(cs_blake3.method, CheckSumMethod::Unknown);
    assert_ne!(cs_sha256.raw, cs_blake3.raw);
    assert_eq!(cs_sha256.method, CheckSumMethod::Sha256);
    assert_eq!(cs_blake3.method, CheckSumMethod::Blake3);

    let empty_cs = CheckSum::empty();
    assert_eq!(empty_cs.method, CheckSumMethod::Unknown);
}

#[test]
fn test_dedup_and_fetch_simple() {
    let (dedup, backend, _g, _l) = setup_test();
    let off = 0;
    let len = CHUNK_SIZE;

    dedup
        .dedup(off as u64, len as u32, None, CheckSumMethod::Blake3)
        .unwrap();

    assert_eq!(backend.state.lock().unwrap().fetch_calls, 1);

    let mut buf = vec![0; len as usize];
    dedup.fetch(off as usize, &mut buf).unwrap();
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 1);

    let mut original_data = vec![0; len as usize];
    backend.state.lock().unwrap().fetch_calls = 0;
    backend.fetch(off as usize, &mut original_data).unwrap();
    assert_eq!(buf, original_data);
}

#[test]
fn test_add_data_registers_chunk_via_local_chunk_server() {
    let (dedup, _backend, index, shutdown, handle, _g, _l, _s) =
        setup_test_with_local_chunk_client();
    let data = b"register-from-add-data";
    let checksum = CheckSum::from_data(data, CheckSumMethod::Blake3);

    dedup.core.add_data(&checksum, data).unwrap();

    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(3);
    while index.registered().is_empty() {
        assert!(
            std::time::Instant::now() < deadline,
            "register was not observed in time"
        );
        std::thread::sleep(std::time::Duration::from_millis(20));
    }
    assert_eq!(index.registered(), vec![checksum]);

    shutdown.shutdown();
    handle.join().unwrap().unwrap();
}

#[test]
fn test_dedup_metrics_emit_store_and_chunk_hit() {
    let harness = MetricsHarness::new();
    let g_store_dir = tempfile::tempdir().unwrap();
    let l_store_dir = tempfile::tempdir().unwrap();
    let backend = Arc::new(MockBackend::new(2 * CHUNK_SIZE as u64));
    let chunk_db = Arc::new(ChunkDB::new(g_store_dir.path()).unwrap());
    let index_db = Arc::new(IndexDB::open(l_store_dir.path()).unwrap());
    let dedup = DedupReader::new_with_metrics(
        backend,
        chunk_db,
        index_db,
        "test-dedup-metrics-hit",
        None,
        DedupMetrics::new(&harness.meter("imagefsd.dedup.test")),
    )
    .unwrap();

    let off = 0;
    let len = CHUNK_SIZE;
    dedup
        .dedup(off as u64, len as u32, None, CheckSumMethod::Blake3)
        .unwrap();

    let mut buffer = vec![0u8; len];
    let read = dedup.fetch(off, &mut buffer).unwrap();
    assert_eq!(read, len);

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.dedup.store_total")
        .contains(&(attrs(&[("result", "ok")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.dedup.store_bytes")
        .contains(&(attrs(&[("result", "ok")]), len as u64)));
    assert!(sum_points_u64(&collected, "imagefsd.dedup.chunk_hit_total").contains(&(attrs(&[]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.dedup.read_total")
        .contains(&(attrs(&[("result", "ok")]), 1)));
    assert!(
        histogram_points_f64(&collected, "imagefsd.dedup.read_duration_ms")
            .iter()
            .any(
                |(point_attrs, count, _)| point_attrs == &attrs(&[("result", "ok")]) && *count == 1
            )
    );
}

#[test]
fn test_dedup_metrics_emit_backend_fallback() {
    let harness = MetricsHarness::new();
    let g_store_dir = tempfile::tempdir().unwrap();
    let l_store_dir = tempfile::tempdir().unwrap();
    let backend = Arc::new(MockBackend::new(2 * CHUNK_SIZE as u64));
    let chunk_db = Arc::new(ChunkDB::new(g_store_dir.path()).unwrap());
    let index_db = Arc::new(IndexDB::open(l_store_dir.path()).unwrap());
    let dedup = DedupReader::new_with_metrics(
        backend,
        chunk_db,
        index_db,
        "test-dedup-metrics-fallback",
        None,
        DedupMetrics::new(&harness.meter("imagefsd.dedup.test")),
    )
    .unwrap();

    let mut buffer = vec![0u8; CHUNK_SIZE];
    let read = dedup.fetch(0, &mut buffer).unwrap();
    assert_eq!(read, CHUNK_SIZE);

    let collected = harness.collect();
    assert!(
        sum_points_u64(&collected, "imagefsd.dedup.backend_fallback_total")
            .contains(&(attrs(&[("reason", "missing_metadata")]), 1))
    );
    assert!(sum_points_u64(&collected, "imagefsd.dedup.read_total")
        .contains(&(attrs(&[("result", "ok")]), 1)));
}

#[test]
fn test_sync_worker_rebuilds_missing_chunk() {
    let (dedup, backend, _g, _l) = setup_test();
    let off = 0;
    let len = CHUNK_SIZE;

    let mut original = vec![0_u8; len];
    backend.fetch(off, &mut original).unwrap();
    let cs = CheckSum::from_data(&original, CheckSumMethod::Blake3);
    let range = DedupRange::new_with_id(dedup.core.id, off as u64, len as u32);
    dedup.core.add_local_dedup_info(&range, &cs).unwrap();

    assert!(!dedup.core.chunk_db.has_chunk(&cs).unwrap());

    let (_tx, rx) = mpsc::sync_channel(1);
    let worker = SyncWorker::new(dedup.core.clone(), rx);
    let info = DedupInfo::new(off as u64, len as u32, cs);
    worker.process(info);

    assert!(dedup.core.chunk_db.has_chunk(&cs).unwrap());
}

#[test]
fn test_deduplication_prevents_refetch() {
    let (dedup, backend, _g, _l) = setup_test();
    let len = CHUNK_SIZE;
    let off1 = 0_u64;
    let off2 = CHUNK_SIZE as u64;

    let data_to_write = (0..len).map(|i| (i % 26) as u8 + b'a').collect::<Vec<_>>();
    {
        let mut state = backend.state.lock().unwrap();
        for i in 0..len {
            state.data.insert(off1 as usize + i, data_to_write[i]);
            state.data.insert(off2 as usize + i, data_to_write[i]);
        }
    }

    dedup
        .dedup(off1, len as u32, None, CheckSumMethod::Sha256)
        .unwrap();
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 1);

    dedup
        .dedup(off2, len as u32, None, CheckSumMethod::Sha256)
        .unwrap();
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 2);

    let mut buf1 = vec![0; len as usize];
    let mut buf2 = vec![0; len as usize];

    dedup.fetch(off1 as usize, &mut buf1).unwrap();
    dedup.fetch(off2 as usize, &mut buf2).unwrap();

    assert_eq!(buf1, data_to_write);
    assert_eq!(buf2, data_to_write);
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 2);
}

#[test]
fn test_fetch_mixed_data() {
    let (dedup, backend, _g, _l) = setup_test();

    dedup
        .dedup(0, CHUNK_SIZE as u32, None, CheckSumMethod::Blake3)
        .unwrap();
    let initial_fetch_count = backend.state.lock().unwrap().fetch_calls;
    assert_eq!(initial_fetch_count, 1);

    std::thread::sleep(std::time::Duration::from_millis(100));
    backend.state.lock().unwrap().fetch_calls = 0;

    let fetch_off = CHUNK_SIZE / 2;
    let fetch_len = CHUNK_SIZE;
    let mut buf = vec![0; fetch_len];
    dedup.fetch(fetch_off, &mut buf).unwrap();

    let fetch_count = backend.state.lock().unwrap().fetch_calls;
    assert!(
        fetch_count >= 1,
        "Expected at least 1 fetch for chunk 1, got {}",
        fetch_count
    );

    let mut original_data = vec![0; fetch_len];
    backend.fetch(fetch_off, &mut original_data).unwrap();
    assert_eq!(buf, original_data);
}

#[test]
fn test_dedup_alignment_check() {
    let (dedup, _backend, _g, _l) = setup_test();

    let result = dedup.dedup(100, CHUNK_SIZE as u32, None, CheckSumMethod::Blake3);
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("not aligned"));

    let result = dedup.dedup(0, 100, None, CheckSumMethod::Blake3);
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("not aligned"));

    let result = dedup.dedup(100, 100, None, CheckSumMethod::Blake3);
    assert!(result.is_err());
}

#[test]
fn test_dedup_short_read_errors() {
    let (dedup, _backend, _g, _l) = setup_test();
    let size = 2 * CHUNK_SIZE as u64;
    let off = size;
    let len = CHUNK_SIZE as u32;

    let result = dedup.dedup(off, len, None, CheckSumMethod::Blake3);
    assert!(result.is_err());
    assert!(result.unwrap_err().to_string().contains("out of range"));
}

#[test]
fn test_send_dedup_request_failure_does_not_mark_chunk() {
    let (mut dedup, _backend, _g, _l) = setup_test();

    let (tx, rx) = std::sync::mpsc::sync_channel(1);
    drop(rx);
    let old_tx = std::mem::replace(&mut dedup.dedup_tx, tx);
    drop(old_tx);

    let chunk_idx = 0;
    assert!(!dedup.core.chunk_is_dedup(chunk_idx));
    dedup.try_dedup_chunk(0, CHUNK_SIZE, CheckSumMethod::Blake3);
    assert!(!dedup.core.chunk_is_dedup(chunk_idx));
}

#[test]
fn test_fetch_truncates_to_backend_size() {
    let (dedup, backend, _g, _l) = setup_test();
    let size = backend.size() as usize;
    let off = size - 10;
    let mut buf = vec![0u8; 100];

    let n = dedup.fetch(off, &mut buf).unwrap();
    assert_eq!(n, 10);

    let mut expected = vec![0u8; 10];
    backend.fetch(off, &mut expected).unwrap();
    assert_eq!(&buf[..10], expected.as_slice());
}

#[test]
fn test_gc_chunk() {
    let (dedup, backend, _g, _l) = setup_test();

    dedup
        .dedup(0, CHUNK_SIZE as u32, None, CheckSumMethod::Blake3)
        .unwrap();

    dedup.gc_chunk(0).unwrap();
    assert!(backend.state.lock().unwrap().invalidate_calls.contains(&0));

    dedup.gc_chunk(1).unwrap();
    assert!(!backend.state.lock().unwrap().invalidate_calls.contains(&1));
}

#[test]
fn test_datarange_ordering() {
    let r1 = DedupRange::new(100, 50);
    let r2 = DedupRange::new(200, 50);
    assert!(r1 < r2);

    let r3 = DedupRange::new(100, 50);
    let r4 = DedupRange::new(100, 100);
    assert!(r3 < r4);

    let r5 = DedupRange::new(100, 50);
    assert_eq!(r1, r5);
}

#[test]
fn test_fetch_partial_deduped_block() {
    let (dedup, backend, _g, _l) = setup_test();
    let off = 0;
    let len = CHUNK_SIZE;

    dedup
        .dedup(off as u64, len as u32, None, CheckSumMethod::Blake3)
        .unwrap();
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 1);

    let fetch_off = CHUNK_SIZE / 4;
    let fetch_len = CHUNK_SIZE / 2;
    let mut buf = vec![0; fetch_len];
    dedup.fetch(fetch_off, &mut buf).unwrap();

    assert_eq!(backend.state.lock().unwrap().fetch_calls, 1);

    let mut original_data = vec![0; fetch_len];
    backend.fetch(fetch_off, &mut original_data).unwrap();
    assert_eq!(buf, original_data);
}

#[test]
fn test_dedup_with_precomputed_checksum() {
    let (dedup, backend, _g, _l) = setup_test();
    let off1 = 0;
    let off2 = CHUNK_SIZE;
    let len = CHUNK_SIZE as u32;

    let mut data = vec![0; len as usize];
    backend.fetch(off1, &mut data).unwrap();
    let cs = CheckSum::from_data(&data, CheckSumMethod::Sha256);

    dedup
        .dedup(off1 as u64, len, None, CheckSumMethod::Sha256)
        .unwrap();
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 2);

    dedup
        .dedup(off2 as u64, len, Some(cs), CheckSumMethod::Sha256)
        .unwrap();
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 2);

    let mut buf = vec![0; len as usize];
    dedup.fetch(off2 as usize, &mut buf).unwrap();
    assert_eq!(buf, data);
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 2);
}

#[test]
fn test_dedup_instances_isolate_by_data_id() {
    let g_store_dir = tempfile::tempdir().unwrap();
    let l_store_dir = tempfile::tempdir().unwrap();
    let backend = Arc::new(MockBackend::new(2 * CHUNK_SIZE as u64));

    let chunk_db = Arc::new(ChunkDB::new(g_store_dir.path()).unwrap());
    let index_db = Arc::new(IndexDB::open(l_store_dir.path()).unwrap());

    let dedup_a = DedupReader::new(
        backend.clone(),
        Arc::clone(&chunk_db),
        Arc::clone(&index_db),
        "img-a",
        None,
    )
    .unwrap();
    let dedup_b = DedupReader::new(
        backend.clone(),
        Arc::clone(&chunk_db),
        Arc::clone(&index_db),
        "img-b",
        None,
    )
    .unwrap();

    let off = 0;
    let len = CHUNK_SIZE as u32;

    dedup_a
        .dedup(off as u64, len, None, CheckSumMethod::Blake3)
        .unwrap();
    assert_eq!(backend.state.lock().unwrap().fetch_calls, 1);

    let infos = dedup_b.core.dedup_range(off as u64, len as usize).unwrap();
    assert_eq!(infos.len(), 1);
    assert!(!infos[0].0.cs.is_valid());
    assert_eq!(infos[0].1, len as u64);
}

#[test]
fn test_fetch_fallback_on_missing_global_data() {
    let (dedup, backend, _g, _l) = setup_test();
    let off = 0;
    let len = CHUNK_SIZE;

    let data_cs = CheckSum::from_data(b"some fake data", CheckSumMethod::Blake3);
    let range = DedupRange::new(off as u64, len as u32);
    dedup.core.add_local_dedup_info(&range, &data_cs).unwrap();

    assert_eq!(backend.state.lock().unwrap().fetch_calls, 0);

    let mut buf = vec![0; len as usize];
    dedup.fetch(off as usize, &mut buf).unwrap();

    assert!(backend.state.lock().unwrap().fetch_calls >= 1);

    let mut original_data = vec![0; len as usize];
    {
        let state = backend.state.lock().unwrap();
        for (idx, byte) in original_data.iter_mut().enumerate() {
            *byte = *state.data.get(&(off + idx)).unwrap_or(&0);
        }
    }
    assert_eq!(buf, original_data);
}
