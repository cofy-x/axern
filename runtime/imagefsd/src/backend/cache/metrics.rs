use opentelemetry::metrics::{Counter, Histogram, Meter};
use opentelemetry::KeyValue;

#[derive(Debug)]
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub(crate) struct CacheMetrics {
    node_id: String,
    read_total: Counter<u64>,
    read_duration: Histogram<f64>,
    read_bytes: Counter<u64>,
    backend_fetch_total: Counter<u64>,
    backend_fetch_bytes: Counter<u64>,
    backend_fetch_duration: Histogram<f64>,
    inflight_wait_duration: Histogram<f64>,
    readahead_duration: Histogram<f64>,
    readahead_bytes: Counter<u64>,
    readahead_chunks: Counter<u64>,
}

impl CacheMetrics {
    pub(crate) fn new(meter: &Meter, node_id: impl Into<String>) -> Self {
        Self {
            node_id: node_id.into(),
            read_total: meter
                .u64_counter("imagefsd.cache.read_total")
                .with_description("Total cache read attempts")
                .init(),
            read_duration: meter
                .f64_histogram("imagefsd.cache.read_duration_ms")
                .with_description("Cache read duration")
                .with_unit("ms")
                .init(),
            read_bytes: meter
                .u64_counter("imagefsd.cache.read_bytes")
                .with_description("Bytes returned by cache reads")
                .with_unit("By")
                .init(),
            backend_fetch_total: meter
                .u64_counter("imagefsd.cache.backend_fetch_total")
                .with_description("Total backend fetches issued by the cache")
                .init(),
            backend_fetch_bytes: meter
                .u64_counter("imagefsd.cache.backend_fetch_bytes")
                .with_description("Bytes fetched from the backend into cache")
                .with_unit("By")
                .init(),
            backend_fetch_duration: meter
                .f64_histogram("imagefsd.cache.backend_fetch_duration_ms")
                .with_description("Duration of backend fetches issued by the cache")
                .with_unit("ms")
                .init(),
            inflight_wait_duration: meter
                .f64_histogram("imagefsd.cache.inflight_wait_duration_ms")
                .with_description("Duration spent waiting for an in-flight cache chunk fetch")
                .with_unit("ms")
                .init(),
            readahead_duration: meter
                .f64_histogram("imagefsd.cache.readahead_duration_seconds")
                .with_description("Duration of a bounded cache readahead task")
                .with_unit("s")
                .init(),
            readahead_bytes: meter
                .u64_counter("imagefsd.cache.readahead_bytes")
                .with_description("Bytes fetched by bounded cache readahead")
                .with_unit("By")
                .init(),
            readahead_chunks: meter
                .u64_counter("imagefsd.cache.readahead_chunks")
                .with_description("Cache chunks processed by bounded readahead")
                .init(),
        }
    }

    pub(crate) fn record_read(&self, result: &'static str, elapsed_ms: f64, bytes: usize) {
        let attrs = [KeyValue::new("result", result)];
        self.read_total.add(1, &attrs);
        self.read_duration.record(elapsed_ms, &attrs);
        if bytes > 0 {
            self.read_bytes.add(bytes as u64, &attrs);
        }
    }

    pub(crate) fn record_backend_fetch(
        &self,
        path: &'static str,
        result: &'static str,
        elapsed_ms: f64,
        bytes: usize,
    ) {
        if path == "foreground" {
            let result_attrs = [KeyValue::new("result", result)];
            self.backend_fetch_total.add(1, &result_attrs);
            if bytes > 0 {
                self.backend_fetch_bytes.add(bytes as u64, &result_attrs);
            }
        }
        self.backend_fetch_duration.record(
            elapsed_ms,
            &[
                KeyValue::new("node_id", self.node_id.clone()),
                KeyValue::new("path", path),
                KeyValue::new("result", result),
            ],
        );
    }

    pub(crate) fn record_inflight_wait(&self, result: &'static str, elapsed_ms: f64) {
        self.inflight_wait_duration.record(
            elapsed_ms,
            &[
                KeyValue::new("node_id", self.node_id.clone()),
                KeyValue::new("result", result),
            ],
        );
    }

    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    pub(crate) fn record_readahead(
        &self,
        result: &'static str,
        elapsed_seconds: f64,
        bytes: usize,
        cached_chunks: usize,
        skipped_chunks: usize,
    ) {
        let attrs = [KeyValue::new("result", result)];
        self.readahead_duration.record(elapsed_seconds, &attrs);
        if bytes > 0 {
            self.readahead_bytes.add(bytes as u64, &attrs);
        }
        if cached_chunks > 0 {
            self.readahead_chunks.add(
                cached_chunks as u64,
                &[
                    KeyValue::new("result", result),
                    KeyValue::new("action", "cached"),
                ],
            );
        }
        if skipped_chunks > 0 {
            self.readahead_chunks.add(
                skipped_chunks as u64,
                &[
                    KeyValue::new("result", result),
                    KeyValue::new("action", "skipped"),
                ],
            );
        }
    }
}
