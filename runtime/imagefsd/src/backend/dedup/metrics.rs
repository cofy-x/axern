use opentelemetry::metrics::{Counter, Histogram, Meter};
use opentelemetry::KeyValue;

#[derive(Debug)]
pub(crate) struct DedupMetrics {
    read_total: Counter<u64>,
    read_duration: Histogram<f64>,
    chunk_hit_total: Counter<u64>,
    backend_fallback_total: Counter<u64>,
    store_total: Counter<u64>,
    store_bytes: Counter<u64>,
}

impl DedupMetrics {
    pub(crate) fn new(meter: &Meter) -> Self {
        Self {
            read_total: meter
                .u64_counter("imagefsd.dedup.read_total")
                .with_description("Total dedup read attempts")
                .build(),
            read_duration: meter
                .f64_histogram("imagefsd.dedup.read_duration_ms")
                .with_description("Dedup read duration")
                .with_unit("ms")
                .build(),
            chunk_hit_total: meter
                .u64_counter("imagefsd.dedup.chunk_hit_total")
                .with_description("Total dedup chunk hits")
                .build(),
            backend_fallback_total: meter
                .u64_counter("imagefsd.dedup.backend_fallback_total")
                .with_description("Total dedup backend fallbacks")
                .build(),
            store_total: meter
                .u64_counter("imagefsd.dedup.store_total")
                .with_description("Total dedup stores into ChunkDB")
                .build(),
            store_bytes: meter
                .u64_counter("imagefsd.dedup.store_bytes")
                .with_description("Bytes stored into ChunkDB by dedup")
                .with_unit("By")
                .build(),
        }
    }

    pub(crate) fn record_read(&self, result: &'static str, elapsed_ms: f64) {
        let attrs = [KeyValue::new("result", result)];
        self.read_total.add(1, &attrs);
        self.read_duration.record(elapsed_ms, &attrs);
    }

    pub(crate) fn record_chunk_hit(&self) {
        self.chunk_hit_total.add(1, &[]);
    }

    pub(crate) fn record_backend_fallback(&self, reason: &'static str) {
        self.backend_fallback_total
            .add(1, &[KeyValue::new("reason", reason)]);
    }

    pub(crate) fn record_store(&self, result: &'static str, bytes: usize) {
        let attrs = [KeyValue::new("result", result)];
        self.store_total.add(1, &attrs);
        if bytes > 0 {
            self.store_bytes.add(bytes as u64, &attrs);
        }
    }
}
