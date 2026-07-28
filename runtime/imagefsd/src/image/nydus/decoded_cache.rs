#![cfg_attr(not(target_os = "linux"), allow(dead_code))]

use crate::backend::chunkdb::CheckSum;
use opentelemetry::global;
use opentelemetry::metrics::{Counter, Histogram, UpDownCounter};
use opentelemetry::KeyValue;
use std::collections::BTreeMap;
use std::io;
use std::sync::{Arc, Condvar, Mutex};
use std::time::Instant;

#[derive(Clone)]
struct CachedError {
    kind: io::ErrorKind,
    message: String,
}

impl From<&io::Error> for CachedError {
    fn from(error: &io::Error) -> Self {
        Self {
            kind: error.kind(),
            message: error.to_string(),
        }
    }
}

impl From<CachedError> for io::Error {
    fn from(error: CachedError) -> Self {
        Self::new(error.kind, error.message)
    }
}

type LoadResult = Result<Arc<[u8]>, CachedError>;

#[derive(Default)]
struct InFlight {
    result: Mutex<Option<LoadResult>>,
    ready: Condvar,
}

enum Entry {
    Loading(Arc<InFlight>),
    Ready { data: Arc<[u8]>, last_used: u64 },
}

#[derive(Default)]
struct CacheState {
    entries: BTreeMap<CheckSum, Entry>,
    ready_bytes: usize,
    clock: u64,
}

struct DecodedChunkMetrics {
    access_total: Counter<u64>,
    load_duration: Histogram<f64>,
    load_bytes: Counter<u64>,
    eviction_total: Counter<u64>,
    current_bytes: UpDownCounter<i64>,
}

impl DecodedChunkMetrics {
    fn new() -> Self {
        let meter = global::meter("imagefsd.nydus");
        Self {
            access_total: meter
                .u64_counter("imagefsd.nydus.decoded_chunk_access_total")
                .with_description("Decoded Nydus chunk cache accesses")
                .build(),
            load_duration: meter
                .f64_histogram("imagefsd.nydus.decoded_chunk_load_duration_ms")
                .with_description("Nydus chunk backend read and decode duration")
                .with_unit("ms")
                .build(),
            load_bytes: meter
                .u64_counter("imagefsd.nydus.decoded_chunk_load_bytes")
                .with_description("Bytes produced by Nydus chunk decoding")
                .with_unit("By")
                .build(),
            eviction_total: meter
                .u64_counter("imagefsd.nydus.decoded_chunk_eviction_total")
                .with_description("Decoded Nydus chunks evicted from the bounded cache")
                .build(),
            current_bytes: meter
                .i64_up_down_counter("imagefsd.nydus.decoded_chunk_current_bytes")
                .with_description("Bytes currently retained by the decoded Nydus chunk cache")
                .with_unit("By")
                .build(),
        }
    }

    fn record_access(&self, result: &'static str) {
        self.access_total.add(1, &[KeyValue::new("result", result)]);
    }

    fn record_load(&self, result: &'static str, started_at: Instant, bytes: usize) {
        let attrs = [KeyValue::new("result", result)];
        self.load_duration
            .record(started_at.elapsed().as_secs_f64() * 1000.0, &attrs);
        if bytes > 0 {
            self.load_bytes.add(bytes as u64, &attrs);
        }
    }
}

/// Coalesces concurrent chunk decodes and retains a small byte-bounded working set.
pub(super) struct DecodedChunkCache {
    max_bytes: usize,
    state: Mutex<CacheState>,
    metrics: DecodedChunkMetrics,
}

#[derive(Debug)]
pub(super) struct DecodedChunk {
    pub(super) bytes: Arc<[u8]>,
}

enum Lookup {
    Hit(Arc<[u8]>),
    Wait(Arc<InFlight>),
    Load,
}

impl DecodedChunkCache {
    pub(super) fn new(max_bytes: usize) -> Self {
        Self {
            max_bytes,
            state: Mutex::new(CacheState::default()),
            metrics: DecodedChunkMetrics::new(),
        }
    }

    pub(super) fn get_or_load<F>(&self, checksum: CheckSum, load: F) -> io::Result<DecodedChunk>
    where
        F: FnOnce() -> io::Result<Vec<u8>>,
    {
        let lookup = {
            let mut state = self.state.lock().unwrap();
            state.clock = state.clock.wrapping_add(1);
            let now = state.clock;
            match state.entries.get_mut(&checksum) {
                Some(Entry::Ready { data, last_used }) => {
                    *last_used = now;
                    Lookup::Hit(Arc::clone(data))
                }
                Some(Entry::Loading(in_flight)) => Lookup::Wait(Arc::clone(in_flight)),
                None => {
                    let in_flight = Arc::new(InFlight::default());
                    state
                        .entries
                        .insert(checksum, Entry::Loading(Arc::clone(&in_flight)));
                    Lookup::Load
                }
            }
        };

        match lookup {
            Lookup::Hit(data) => {
                self.metrics.record_access("hit");
                return Ok(DecodedChunk { bytes: data });
            }
            Lookup::Wait(in_flight) => {
                self.metrics.record_access("wait");
                let mut result = in_flight.result.lock().unwrap();
                while result.is_none() {
                    result = in_flight.ready.wait(result).unwrap();
                }
                return result
                    .as_ref()
                    .unwrap()
                    .clone()
                    .map(|data| DecodedChunk { bytes: data })
                    .map_err(Into::into);
            }
            Lookup::Load => {}
        }

        self.metrics.record_access("load");
        let started_at = Instant::now();
        let loaded = load().map(Arc::<[u8]>::from);
        self.metrics.record_load(
            if loaded.is_ok() { "ok" } else { "error" },
            started_at,
            loaded.as_ref().map_or(0, |data| data.len()),
        );
        let published = loaded.as_ref().map(Arc::clone).map_err(CachedError::from);

        let (in_flight, retained_bytes, evicted_bytes, evictions) = {
            let mut state = self.state.lock().unwrap();
            let in_flight = match state.entries.get(&checksum) {
                Some(Entry::Loading(in_flight)) => Arc::clone(in_flight),
                _ => unreachable!("decoded chunk load must retain its in-flight entry"),
            };
            *in_flight.result.lock().unwrap() = Some(published);
            let (retained_bytes, evicted_bytes, evictions) = match &loaded {
                Ok(data) if self.max_bytes > 0 && data.len() <= self.max_bytes => {
                    state.clock = state.clock.wrapping_add(1);
                    let last_used = state.clock;
                    state.ready_bytes = state.ready_bytes.saturating_add(data.len());
                    state.entries.insert(
                        checksum,
                        Entry::Ready {
                            data: Arc::clone(data),
                            last_used,
                        },
                    );
                    let (evicted_bytes, evictions) = self.evict_to_limit(&mut state);
                    (data.len(), evicted_bytes, evictions)
                }
                _ => {
                    state.entries.remove(&checksum);
                    (0, 0, 0)
                }
            };
            (in_flight, retained_bytes, evicted_bytes, evictions)
        };

        in_flight.ready.notify_all();
        self.record_cache_change(retained_bytes, evicted_bytes, evictions);

        loaded.map(|data| DecodedChunk { bytes: data })
    }

    pub(super) fn invalidate(&self, checksum: &CheckSum) {
        let removed_bytes = {
            let mut state = self.state.lock().unwrap();
            match state.entries.remove(checksum) {
                Some(Entry::Ready { data, .. }) => {
                    state.ready_bytes = state.ready_bytes.saturating_sub(data.len());
                    data.len()
                }
                _ => 0,
            }
        };
        if removed_bytes > 0 {
            self.metrics.current_bytes.add(-(removed_bytes as i64), &[]);
        }
    }

    fn evict_to_limit(&self, state: &mut CacheState) -> (usize, u64) {
        let mut evicted_bytes = 0usize;
        let mut evictions = 0u64;
        while state.ready_bytes > self.max_bytes {
            let oldest = state
                .entries
                .iter()
                .filter_map(|(checksum, entry)| match entry {
                    Entry::Ready { last_used, .. } => Some((*checksum, *last_used)),
                    Entry::Loading(_) => None,
                })
                .min_by_key(|(_, last_used)| *last_used)
                .map(|(checksum, _)| checksum);
            let Some(oldest) = oldest else {
                break;
            };
            if let Some(Entry::Ready { data, .. }) = state.entries.remove(&oldest) {
                state.ready_bytes = state.ready_bytes.saturating_sub(data.len());
                evicted_bytes = evicted_bytes.saturating_add(data.len());
                evictions = evictions.saturating_add(1);
            }
        }
        (evicted_bytes, evictions)
    }

    fn record_cache_change(&self, retained_bytes: usize, evicted_bytes: usize, evictions: u64) {
        let current_delta = retained_bytes as i64 - evicted_bytes as i64;
        if current_delta != 0 {
            self.metrics.current_bytes.add(current_delta, &[]);
        }
        if evictions > 0 {
            self.metrics.eviction_total.add(evictions, &[]);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{DecodedChunkCache, Entry};
    use crate::backend::chunkdb::{CheckSum, CheckSumMethod};
    use std::io;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::{Arc, Barrier};
    use std::thread;

    fn checksum(value: u8) -> CheckSum {
        CheckSum::from_data(&[value], CheckSumMethod::Blake3)
    }

    #[test]
    fn reuses_a_loaded_chunk() {
        let cache = DecodedChunkCache::new(8);
        let loads = AtomicUsize::new(0);

        for _ in 0..2 {
            let data = cache
                .get_or_load(checksum(1), || {
                    loads.fetch_add(1, Ordering::Relaxed);
                    Ok(vec![1; 4])
                })
                .unwrap();
            assert_eq!(data.bytes.as_ref(), &[1; 4]);
        }

        assert_eq!(loads.load(Ordering::Relaxed), 1);
    }

    #[test]
    fn coalesces_concurrent_loads() {
        let cache = Arc::new(DecodedChunkCache::new(8));
        let loads = Arc::new(AtomicUsize::new(0));
        let barrier = Arc::new(Barrier::new(4));
        let threads = (0..4)
            .map(|_| {
                let cache = Arc::clone(&cache);
                let loads = Arc::clone(&loads);
                let barrier = Arc::clone(&barrier);
                thread::spawn(move || {
                    barrier.wait();
                    cache
                        .get_or_load(checksum(2), || {
                            loads.fetch_add(1, Ordering::Relaxed);
                            thread::sleep(std::time::Duration::from_millis(20));
                            Ok(vec![2; 4])
                        })
                        .unwrap()
                })
            })
            .collect::<Vec<_>>();

        for thread in threads {
            assert_eq!(thread.join().unwrap().bytes.as_ref(), &[2; 4]);
        }
        assert_eq!(loads.load(Ordering::Relaxed), 1);
    }

    #[test]
    fn evicts_by_total_bytes() {
        let cache = DecodedChunkCache::new(4);
        cache.get_or_load(checksum(1), || Ok(vec![1; 4])).unwrap();
        cache.get_or_load(checksum(2), || Ok(vec![2; 4])).unwrap();
        let loads = AtomicUsize::new(0);

        cache
            .get_or_load(checksum(1), || {
                loads.fetch_add(1, Ordering::Relaxed);
                Ok(vec![1; 4])
            })
            .unwrap();

        assert_eq!(loads.load(Ordering::Relaxed), 1);
    }

    #[test]
    fn invalidation_releases_a_persisted_chunk() {
        let cache = DecodedChunkCache::new(8);
        let key = checksum(3);
        cache.get_or_load(key, || Ok(vec![3; 4])).unwrap();
        cache.invalidate(&key);

        let loads = AtomicUsize::new(0);
        cache
            .get_or_load(key, || {
                loads.fetch_add(1, Ordering::Relaxed);
                Ok(vec![3; 4])
            })
            .unwrap();
        assert_eq!(loads.load(Ordering::Relaxed), 1);
    }

    #[test]
    fn errors_are_not_cached() {
        let cache = DecodedChunkCache::new(8);
        let first = cache.get_or_load(checksum(1), || {
            Err(io::Error::new(io::ErrorKind::TimedOut, "load failed"))
        });
        assert_eq!(first.unwrap_err().kind(), io::ErrorKind::TimedOut);

        assert_eq!(
            cache
                .get_or_load(checksum(1), || Ok(vec![3; 4]))
                .unwrap()
                .bytes
                .as_ref(),
            &[3; 4]
        );
    }

    #[test]
    fn coalesces_when_retention_is_disabled() {
        let cache = Arc::new(DecodedChunkCache::new(0));
        let loads = Arc::new(AtomicUsize::new(0));
        let entered = Arc::new(Barrier::new(2));
        let release = Arc::new(Barrier::new(2));

        let loader_cache = Arc::clone(&cache);
        let loader_loads = Arc::clone(&loads);
        let loader_entered = Arc::clone(&entered);
        let loader_release = Arc::clone(&release);
        let loader = thread::spawn(move || {
            loader_cache.get_or_load(checksum(4), || {
                loader_loads.fetch_add(1, Ordering::Relaxed);
                loader_entered.wait();
                loader_release.wait();
                Ok(vec![4; 4])
            })
        });

        entered.wait();
        let waiter_cache = Arc::clone(&cache);
        let waiter_loads = Arc::clone(&loads);
        let waiter = thread::spawn(move || {
            waiter_cache.get_or_load(checksum(4), || {
                waiter_loads.fetch_add(1, Ordering::Relaxed);
                Ok(vec![4; 4])
            })
        });
        while {
            let state = cache.state.lock().unwrap();
            match state.entries.get(&checksum(4)) {
                Some(Entry::Loading(in_flight)) => Arc::strong_count(in_flight) < 2,
                _ => true,
            }
        } {
            thread::yield_now();
        }
        release.wait();

        assert_eq!(loader.join().unwrap().unwrap().bytes.as_ref(), &[4; 4]);
        assert_eq!(waiter.join().unwrap().unwrap().bytes.as_ref(), &[4; 4]);
        assert_eq!(loads.load(Ordering::Relaxed), 1);
    }
}
