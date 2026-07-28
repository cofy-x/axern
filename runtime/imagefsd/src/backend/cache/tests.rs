use super::*;
use crate::test_metrics::{histogram_points_f64, sum_points_u64, MetricsHarness};
use std::collections::BTreeMap;
use std::io::{Read, Seek};
use std::sync::atomic::AtomicUsize;
use std::sync::{Condvar, Mutex};
use std::thread;
use std::time::{Duration, Instant};
use tempfile::TempDir;

#[derive(Debug)]
struct MockBackend {
    source_data: Vec<u8>,
    fetch_count: AtomicUsize,
    fetched_chunks: Mutex<Vec<usize>>,
    fetch_delay: Option<Duration>,
}

impl MockBackend {
    fn new(size: usize) -> Self {
        let mut source_data = Vec::with_capacity(size);
        for i in 0..size {
            source_data.push((i % 256) as u8);
        }
        Self {
            source_data,
            fetch_count: AtomicUsize::new(0),
            fetched_chunks: Mutex::new(Vec::new()),
            fetch_delay: None,
        }
    }

    fn with_delay(mut self, delay: Duration) -> Self {
        self.fetch_delay = Some(delay);
        self
    }
}

impl Backend for MockBackend {
    fn size(&self) -> u64 {
        self.source_data.len() as u64
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        if let Some(delay) = self.fetch_delay {
            thread::sleep(delay);
        }

        self.fetch_count.fetch_add(1, SeqCst);
        let chunk_idx = off / CHUNK_SIZE;
        self.fetched_chunks.lock().unwrap().push(chunk_idx);

        let end = off + data.len();
        if off >= self.source_data.len() || end > self.source_data.len() {
            return Err(new_std_io_error(std::io::ErrorKind::InvalidInput));
        }

        data.copy_from_slice(&self.source_data[off..end]);
        Ok(data.len())
    }
}

#[derive(Debug)]
struct ForegroundPriorityBackend {
    data: Vec<u8>,
    calls: AtomicUsize,
    first_call: (Mutex<(bool, bool)>, Condvar),
}

impl ForegroundPriorityBackend {
    fn new(size: usize) -> Self {
        Self {
            data: vec![7; size],
            calls: AtomicUsize::new(0),
            first_call: (Mutex::new((false, false)), Condvar::new()),
        }
    }

    fn wait_for_first_call(&self) {
        let (state, changed) = &self.first_call;
        let mut state = state.lock().unwrap();
        while !state.0 {
            state = changed.wait(state).unwrap();
        }
    }

    fn release_first_call(&self) {
        let (state, changed) = &self.first_call;
        let mut state = state.lock().unwrap();
        state.1 = true;
        changed.notify_all();
    }
}

impl Backend for ForegroundPriorityBackend {
    fn size(&self) -> u64 {
        self.data.len() as u64
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        if self.calls.fetch_add(1, SeqCst) == 0 {
            let (state, changed) = &self.first_call;
            let mut state = state.lock().unwrap();
            state.0 = true;
            changed.notify_all();
            while !state.1 {
                state = changed.wait(state).unwrap();
            }
        }
        data.copy_from_slice(&self.data[off..off + data.len()]);
        Ok(data.len())
    }
}

#[derive(Debug)]
struct FlakyBackend {
    data: Vec<u8>,
    fail_once: std::sync::atomic::AtomicBool,
}

#[derive(Debug)]
struct ShortReadBackend {
    size: usize,
}

#[derive(Debug)]
struct PartialReadBackend {
    data: Vec<u8>,
}

impl Backend for PartialReadBackend {
    fn size(&self) -> u64 {
        self.data.len() as u64
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        let read = data.len().min(1024);
        data[..read].copy_from_slice(&self.data[off..off + read]);
        Ok(read)
    }
}

impl Backend for ShortReadBackend {
    fn size(&self) -> u64 {
        self.size as u64
    }

    fn fetch(&self, _off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        Ok(data.len().saturating_sub(1))
    }
}

impl FlakyBackend {
    fn new(size: usize) -> Self {
        let mut data = Vec::with_capacity(size);
        for i in 0..size {
            data.push((i % 256) as u8);
        }
        Self {
            data,
            fail_once: std::sync::atomic::AtomicBool::new(true),
        }
    }
}

impl Backend for FlakyBackend {
    fn size(&self) -> u64 {
        self.data.len() as u64
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> std::io::Result<usize> {
        if self
            .fail_once
            .compare_exchange(true, false, SeqCst, SeqCst)
            .is_ok()
        {
            return Err(new_std_io_error(std::io::ErrorKind::Other));
        }
        let end = off + data.len();
        if off >= self.data.len() || end > self.data.len() {
            return Err(new_std_io_error(std::io::ErrorKind::InvalidInput));
        }
        data.copy_from_slice(&self.data[off..end]);
        Ok(data.len())
    }
}

struct TestEnv {
    _temp_dir: TempDir,
    cache_path: String,
    bitmap_path: String,
}

impl TestEnv {
    fn new() -> Self {
        let temp_dir = TempDir::new().unwrap();
        let cache_file_path = temp_dir.path().join("cache.raw");
        let cache_path = cache_file_path.to_str().unwrap().to_string();
        let bitmap_path = format!("{}.bitmap", cache_path);
        Self {
            _temp_dir: temp_dir,
            cache_path,
            bitmap_path,
        }
    }
}

fn attrs(pairs: &[(&str, &str)]) -> BTreeMap<String, String> {
    pairs
        .iter()
        .map(|(key, value)| ((*key).to_string(), (*value).to_string()))
        .collect()
}

#[test]
fn test_cache_new_and_creation() {
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE * 5);
    let _cache = Cache::new(backend, &env.cache_path).unwrap();

    let raw_meta = std::fs::metadata(&env.cache_path).unwrap();
    let bitmap_meta = std::fs::metadata(&env.bitmap_path).unwrap();

    assert_eq!(raw_meta.len(), CHUNK_SIZE as u64 * 5);
    assert_eq!(bitmap_meta.len(), page_size());
}

#[test]
fn test_cache_new_with_existing_files() {
    let env = TestEnv::new();
    let backend_size = CHUNK_SIZE * 3;

    {
        let file = File::create(&env.cache_path).unwrap();
        file.set_len((CHUNK_SIZE * 2) as u64).unwrap();
    }

    let backend = MockBackend::new(backend_size);
    let _cache = Cache::new(backend, &env.cache_path).unwrap();

    let raw_meta = std::fs::metadata(&env.cache_path).unwrap();
    assert_eq!(raw_meta.len(), backend_size as u64);
}

#[test]
fn test_read_at_cold_cache() {
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE * 2);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let mut buffer = vec![0u8; 128];
    let offset = 10;

    let n = cache.read_at(offset, &mut buffer).unwrap();
    assert_eq!(n, 128);

    assert_eq!(buffer, &cache.b.source_data[offset..offset + 128]);
    assert_eq!(cache.b.fetch_count.load(Relaxed), 1);
    assert_eq!(*cache.b.fetched_chunks.lock().unwrap(), vec![0]);
}

#[test]
fn test_read_at_hot_cache() {
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE * 2);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let mut buffer = vec![0u8; 128];
    let offset = 10;

    cache.read_at(offset, &mut buffer).unwrap();
    assert_eq!(cache.b.fetch_count.load(Relaxed), 1);

    cache.b.fetch_count.store(0, Relaxed);

    let n = cache.read_at(offset, &mut buffer).unwrap();
    assert_eq!(n, 128);
    assert_eq!(buffer, &cache.b.source_data[offset..offset + 128]);
    assert_eq!(cache.b.fetch_count.load(Relaxed), 0);
}

#[test]
fn test_read_spanning_multiple_chunks() {
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE * 3);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let offset = CHUNK_SIZE - 64;
    let len = 128;
    let mut buffer = vec![0u8; len];

    let n = cache.read_at(offset, &mut buffer).unwrap();
    assert_eq!(n, len);

    assert_eq!(buffer, &cache.b.source_data[offset..offset + len]);
    assert_eq!(cache.b.fetch_count.load(Relaxed), 2);
    let mut fetched = cache.b.fetched_chunks.lock().unwrap();
    fetched.sort();
    assert_eq!(*fetched, vec![0, 1]);
}

#[test]
fn test_read_at_concurrent_fetches() {
    let env = TestEnv::new();
    let backend = Arc::new(MockBackend::new(CHUNK_SIZE * 3).with_delay(Duration::from_millis(50)));
    let cache = Arc::new(Cache::new(Arc::clone(&backend), &env.cache_path).unwrap());

    let mut handles = vec![];
    let num_threads = 10;

    for _ in 0..num_threads {
        let cache_clone = Arc::clone(&cache);
        let handle = thread::spawn(move || {
            let mut buffer = vec![0u8; 256];
            let offset = CHUNK_SIZE + 100;
            let n = cache_clone.read_at(offset, &mut buffer).unwrap();
            (n, buffer)
        });
        handles.push(handle);
    }

    for handle in handles {
        let (n, buffer) = handle.join().unwrap();
        assert_eq!(n, 256);
        let offset = CHUNK_SIZE + 100;
        assert_eq!(buffer, &backend.source_data[offset..offset + 256]);
    }

    assert_eq!(backend.fetch_count.load(Relaxed), 1);
    assert_eq!(*backend.fetched_chunks.lock().unwrap(), vec![1]);
}

#[test]
fn test_read_at_boundary_conditions() {
    let env = TestEnv::new();
    let size = 1024;
    let backend = MockBackend::new(size);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let offset = size - 64;
    let len = 64;
    let mut buffer1 = vec![0u8; len];
    let n1 = cache.read_at(offset, &mut buffer1).unwrap();
    assert_eq!(n1, len);
    assert_eq!(buffer1, &cache.b.source_data[offset..]);

    let offset_over = size - 32;
    let len_over = 128;
    let mut buffer2 = vec![0u8; len_over];
    let n2 = cache.read_at(offset_over, &mut buffer2).unwrap();

    assert_eq!(n2, 32);
    assert_eq!(&buffer2[..32], &cache.b.source_data[offset_over..]);
}

#[test]
fn test_cache_metrics_emit_successful_read_and_backend_fetch() {
    let harness = MetricsHarness::new();
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE * 2);
    let mut cache = Cache::new(backend, &env.cache_path).unwrap();
    cache.metrics = CacheMetrics::new(&harness.meter("imagefsd.cache.test"), "node-test");

    let mut buffer = vec![0u8; 128];
    let read = cache.read_at(0, &mut buffer).unwrap();
    assert_eq!(read, 128);

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.cache.read_total")
        .contains(&(attrs(&[("result", "ok")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.cache.read_bytes")
        .contains(&(attrs(&[("result", "ok")]), 128)));
    assert!(
        sum_points_u64(&collected, "imagefsd.cache.backend_fetch_total")
            .contains(&(attrs(&[("result", "ok")]), 1))
    );
    assert!(
        sum_points_u64(&collected, "imagefsd.cache.backend_fetch_bytes")
            .contains(&(attrs(&[("result", "ok")]), CHUNK_SIZE as u64))
    );
    assert!(
        histogram_points_f64(&collected, "imagefsd.cache.read_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs == &attrs(&[("result", "ok")]) && *count == 1
            })
    );
    assert!(
        histogram_points_f64(&collected, "imagefsd.cache.backend_fetch_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs
                    == &attrs(&[
                        ("node_id", "node-test"),
                        ("path", "foreground"),
                        ("result", "ok"),
                    ])
                    && *count == 1
            })
    );
}

#[test]
fn test_cache_metrics_emit_error_results() {
    let harness = MetricsHarness::new();

    let env = TestEnv::new();
    let backend = FlakyBackend::new(CHUNK_SIZE);
    let mut cache = Cache::new(backend, &env.cache_path).unwrap();
    cache.metrics = CacheMetrics::new(&harness.meter("imagefsd.cache.test"), "node-test");
    let mut buffer = vec![0u8; 64];
    assert!(matches!(
        cache.read_at(0, &mut buffer),
        Err(CacheError::IoError(_))
    ));

    let env_invalid = TestEnv::new();
    let backend_invalid = MockBackend::new(CHUNK_SIZE);
    let mut cache_invalid = Cache::new(backend_invalid, &env_invalid.cache_path).unwrap();
    cache_invalid.metrics = CacheMetrics::new(&harness.meter("imagefsd.cache.test"), "node-test");
    assert!(matches!(
        cache_invalid.read_at(CHUNK_SIZE + 1, &mut buffer),
        Err(CacheError::InvalidRange)
    ));

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.cache.read_total")
        .contains(&(attrs(&[("result", "io_error")]), 1)));
    assert!(
        sum_points_u64(&collected, "imagefsd.cache.backend_fetch_total")
            .contains(&(attrs(&[("result", "io_error")]), 1))
    );
    assert!(sum_points_u64(&collected, "imagefsd.cache.read_total")
        .contains(&(attrs(&[("result", "invalid_range")]), 1)));
}

#[test]
fn test_cache_rejects_short_backend_read_without_publishing_chunk() {
    let harness = MetricsHarness::new();
    let env = TestEnv::new();
    let mut cache = Cache::new(ShortReadBackend { size: CHUNK_SIZE }, &env.cache_path).unwrap();
    cache.metrics = CacheMetrics::new(&harness.meter("imagefsd.cache.test"), "node-test");

    let mut buffer = vec![0u8; 64];
    let error = cache.read_at(0, &mut buffer).unwrap_err();
    assert!(matches!(
        error,
        CacheError::IoError(ref error) if error.kind() == std::io::ErrorKind::UnexpectedEof
    ));
    assert_eq!(cache.bitmap.as_slice()[0] & 1, 0);

    let collected = harness.collect();
    assert!(
        histogram_points_f64(&collected, "imagefsd.cache.backend_fetch_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs
                    == &attrs(&[
                        ("node_id", "node-test"),
                        ("path", "foreground"),
                        ("result", "short_read"),
                    ])
                    && *count == 1
            })
    );
}

#[test]
fn test_cache_fills_chunk_across_partial_backend_reads() {
    let env = TestEnv::new();
    let data = (0..CHUNK_SIZE)
        .map(|index| (index % 251) as u8)
        .collect::<Vec<_>>();
    let cache = Cache::new(PartialReadBackend { data: data.clone() }, &env.cache_path).unwrap();

    let mut buffer = vec![0u8; 64];
    let read = cache.read_at(100, &mut buffer).unwrap();

    assert_eq!(read, buffer.len());
    assert_eq!(buffer, data[100..164]);
    assert_eq!(cache.bitmap.as_slice()[0] & 1, 1);
}

#[test]
fn test_cache_metrics_attribute_inflight_waiters() {
    let harness = MetricsHarness::new();
    let env = TestEnv::new();
    let backend = Arc::new(ForegroundPriorityBackend::new(CHUNK_SIZE));
    let mut cache = Cache::new(Arc::clone(&backend), &env.cache_path).unwrap();
    cache.metrics = CacheMetrics::new(&harness.meter("imagefsd.cache.test"), "node-test");
    let cache = Arc::new(cache);

    let leader_cache = Arc::clone(&cache);
    let leader = thread::spawn(move || {
        leader_cache.read_at(0, &mut [0u8; 64]).unwrap();
    });
    backend.wait_for_first_call();

    let handles = (0..7)
        .map(|_| {
            let cache = Arc::clone(&cache);
            thread::spawn(move || {
                cache.read_at(0, &mut [0u8; 64]).unwrap();
            })
        })
        .collect::<Vec<_>>();
    let deadline = Instant::now() + Duration::from_secs(5);
    loop {
        let registered = cache
            .in_flight_ios
            .lock()
            .unwrap()
            .get(&0)
            .map(Arc::strong_count)
            .unwrap_or_default();
        if registered >= 9 {
            break;
        }
        assert!(
            Instant::now() < deadline,
            "waiters did not join the in-flight fetch"
        );
        thread::yield_now();
    }
    backend.release_first_call();
    leader.join().unwrap();
    for handle in handles {
        handle.join().unwrap();
    }

    let collected = harness.collect();
    assert!(
        histogram_points_f64(&collected, "imagefsd.cache.inflight_wait_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs == &attrs(&[("node_id", "node-test"), ("result", "notified")])
                    && *count == 7
            })
    );
}

#[test]
fn test_read_at_invalid_range() {
    let env = TestEnv::new();
    let size = 1024;
    let backend = MockBackend::new(size);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let mut buffer = vec![0u8; 64];
    let result = cache.read_at(size + 1, &mut buffer);
    assert!(matches!(result, Err(CacheError::InvalidRange)));
}

#[test]
fn test_read_at_zero_len_is_noop() {
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let mut buffer = vec![];
    let n = cache.read_at(0, &mut buffer).unwrap();
    assert_eq!(n, 0);
    assert_eq!(cache.b.fetch_count.load(Relaxed), 0);
}

#[test]
fn test_readahead_populates_bounded_range() {
    let env = TestEnv::new();
    let size = CHUNK_SIZE * 2 + 123;
    let backend = MockBackend::new(size);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let outcome = cache
        .readahead(CHUNK_SIZE, CHUNK_SIZE + 123, &AtomicBool::new(false))
        .unwrap();
    assert_eq!(outcome.bytes, CHUNK_SIZE + 123);
    assert_eq!(outcome.cached_chunks, 2);
    assert_eq!(outcome.skipped_chunks, 0);

    let mut data = vec![0_u8; CHUNK_SIZE + 123];
    assert_eq!(cache.read_at(CHUNK_SIZE, &mut data).unwrap(), data.len());
    assert_eq!(data, cache.b.source_data[CHUNK_SIZE..]);
    assert_eq!(cache.b.fetch_count.load(Relaxed), 2);
}

#[test]
fn test_readahead_skips_chunks_already_fetched_on_demand() {
    let env = TestEnv::new();
    let size = CHUNK_SIZE * 2;
    let backend = MockBackend::new(size);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let mut first_byte = [0_u8; 1];
    cache.read_at(0, &mut first_byte).unwrap();
    let outcome = cache.readahead(0, size, &AtomicBool::new(false)).unwrap();

    assert_eq!(outcome.bytes, CHUNK_SIZE);
    assert_eq!(outcome.cached_chunks, 1);
    assert_eq!(outcome.skipped_chunks, 1);
}

#[test]
fn test_readahead_honors_cancellation_before_fetch() {
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE);
    let cache = Cache::new(backend, &env.cache_path).unwrap();
    let cancelled = AtomicBool::new(true);

    let outcome = cache.readahead(0, CHUNK_SIZE, &cancelled).unwrap();

    assert_eq!(outcome, ReadaheadOutcome::default());
    assert_eq!(cache.b.fetch_count.load(Relaxed), 0);
}

#[test]
fn test_foreground_miss_does_not_wait_for_readahead_remote_io() {
    let env = TestEnv::new();
    let cache =
        Arc::new(Cache::new(ForegroundPriorityBackend::new(CHUNK_SIZE), &env.cache_path).unwrap());
    let background = {
        let cache = Arc::clone(&cache);
        thread::spawn(move || {
            cache
                .readahead(0, CHUNK_SIZE, &AtomicBool::new(false))
                .unwrap()
        })
    };
    cache.b.wait_for_first_call();

    let mut foreground = [0_u8; 1];
    assert_eq!(cache.read_at(0, &mut foreground).unwrap(), 1);
    assert_eq!(foreground, [7]);

    cache.b.release_first_call();
    let outcome = background.join().unwrap();
    assert_eq!(outcome.cached_chunks, 0);
    assert_eq!(outcome.skipped_chunks, 1);
}

#[test]
fn test_fetch_error_clears_inflight() {
    let env = TestEnv::new();
    let backend = FlakyBackend::new(CHUNK_SIZE);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let mut buffer = vec![0u8; 64];
    let first = cache.read_at(0, &mut buffer);
    assert!(matches!(first, Err(CacheError::IoError(_))));

    let second = cache.read_at(0, &mut buffer).unwrap();
    assert_eq!(second, 64);
}

#[test]
fn test_invalidate_and_refetch() {
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE * 3);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    let mut buffer = vec![0u8; 128];
    let offset = CHUNK_SIZE + 50;

    cache.read_at(offset, &mut buffer).unwrap();
    assert_eq!(cache.b.fetch_count.load(Relaxed), 1);
    assert_eq!(*cache.b.fetched_chunks.lock().unwrap(), vec![1]);

    cache.invalidate_chunk(1).unwrap();

    let mut raw_file = File::open(&env.cache_path).unwrap();
    raw_file
        .seek(std::io::SeekFrom::Start(offset as u64))
        .unwrap();
    let mut hole_buffer = vec![0u8; 128];
    raw_file.read_exact(&mut hole_buffer).unwrap();
    assert!(hole_buffer.iter().all(|&x| x == 0));

    let n = cache.read_at(offset, &mut buffer).unwrap();
    assert_eq!(n, 128);
    assert_eq!(buffer, &cache.b.source_data[offset..offset + 128]);
    assert_eq!(cache.b.fetch_count.load(Relaxed), 2);

    let mut fetched_chunks = cache.b.fetched_chunks.lock().unwrap();
    fetched_chunks.sort();
    assert_eq!(*fetched_chunks, vec![1, 1]);
}

#[test]
fn test_invalidate_clears_bitmap() {
    let env = TestEnv::new();
    let backend = MockBackend::new(CHUNK_SIZE * 8);
    let cache = Cache::new(backend, &env.cache_path).unwrap();

    cache.fetch_data(0, CHUNK_SIZE * 8).unwrap();

    let bitmap_data = cache.bitmap.as_slice();
    assert_eq!(bitmap_data[0], 0b11111111);

    cache.invalidate_chunk(2).unwrap();
    cache.invalidate_chunk(3).unwrap();
    cache.invalidate_chunk(4).unwrap();

    let expected_bitmap = 0b1110_0011;
    assert_eq!(bitmap_data[0], expected_bitmap);
}
