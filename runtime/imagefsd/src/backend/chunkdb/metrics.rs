use opentelemetry::metrics::{Counter, Histogram, Meter};
use opentelemetry::KeyValue;

#[derive(Debug)]
pub(super) struct ChunkDbMetrics {
    get_total: Counter<u64>,
    get_duration: Histogram<f64>,
    get_bytes: Counter<u64>,
    add_total: Counter<u64>,
    add_bytes: Counter<u64>,
    touch_total: Counter<u64>,
    gc_removed_total: Counter<u64>,
    gc_duration: Histogram<f64>,
}

impl ChunkDbMetrics {
    pub(super) fn new(meter: &Meter) -> Self {
        Self {
            get_total: meter
                .u64_counter("imagefsd.chunkdb.get_total")
                .with_description("Total ChunkDB read attempts")
                .init(),
            get_duration: meter
                .f64_histogram("imagefsd.chunkdb.get_duration_ms")
                .with_description("ChunkDB read duration")
                .with_unit("ms")
                .init(),
            get_bytes: meter
                .u64_counter("imagefsd.chunkdb.get_bytes")
                .with_description("Bytes read from ChunkDB")
                .with_unit("By")
                .init(),
            add_total: meter
                .u64_counter("imagefsd.chunkdb.add_total")
                .with_description("Total ChunkDB add attempts")
                .init(),
            add_bytes: meter
                .u64_counter("imagefsd.chunkdb.add_bytes")
                .with_description("Bytes added to ChunkDB")
                .with_unit("By")
                .init(),
            touch_total: meter
                .u64_counter("imagefsd.chunkdb.touch_total")
                .with_description("Total ChunkDB touch attempts")
                .init(),
            gc_removed_total: meter
                .u64_counter("imagefsd.chunkdb.gc_removed_total")
                .with_description("Chunks removed by ChunkDB GC")
                .init(),
            gc_duration: meter
                .f64_histogram("imagefsd.chunkdb.gc_duration_ms")
                .with_description("ChunkDB GC duration")
                .with_unit("ms")
                .init(),
        }
    }

    pub(super) fn record_get(&self, result: &'static str, elapsed_ms: f64, bytes: usize) {
        let attrs = [KeyValue::new("result", result)];
        self.get_total.add(1, &attrs);
        self.get_duration.record(elapsed_ms, &attrs);
        if bytes > 0 {
            self.get_bytes.add(bytes as u64, &attrs);
        }
    }

    pub(super) fn record_add(&self, result: &'static str, count: u64, bytes: u64) {
        let attrs = [KeyValue::new("result", result)];
        self.add_total.add(count, &attrs);
        if bytes > 0 {
            self.add_bytes.add(bytes, &attrs);
        }
    }

    pub(super) fn record_touch(&self, result: &'static str) {
        self.touch_total.add(1, &[KeyValue::new("result", result)]);
    }

    pub(super) fn record_gc(
        &self,
        mode: &'static str,
        result: &'static str,
        elapsed_ms: f64,
        removed: usize,
    ) {
        let attrs = [KeyValue::new("mode", mode), KeyValue::new("result", result)];
        self.gc_duration.record(elapsed_ms, &attrs);
        if removed > 0 {
            self.gc_removed_total.add(removed as u64, &attrs);
        }
    }
}
