use opentelemetry::global;
use opentelemetry::metrics::{Counter, Histogram};
use opentelemetry::KeyValue;

pub mod nydus;
pub mod raw;

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
#[derive(Debug)]
pub(crate) struct FsReadMetrics {
    image_type: &'static str,
    read_total: Counter<u64>,
    read_duration: Histogram<f64>,
    read_bytes: Counter<u64>,
}

impl FsReadMetrics {
    pub(crate) fn new(image_type: &'static str) -> Self {
        let meter = global::meter("imagefsd.fs");
        Self::with_meter(&meter, image_type)
    }

    pub(crate) fn with_meter(
        meter: &opentelemetry::metrics::Meter,
        image_type: &'static str,
    ) -> Self {
        Self {
            image_type,
            read_total: meter
                .u64_counter("imagefsd.fs.read_total")
                .with_description("Total filesystem read attempts")
                .init(),
            read_duration: meter
                .f64_histogram("imagefsd.fs.read_duration_ms")
                .with_description("Filesystem read duration")
                .with_unit("ms")
                .init(),
            read_bytes: meter
                .u64_counter("imagefsd.fs.read_bytes")
                .with_description("Bytes returned by filesystem reads")
                .with_unit("By")
                .init(),
        }
    }

    #[cfg_attr(not(target_os = "linux"), allow(dead_code))]
    pub(crate) fn record_read(&self, result: &'static str, elapsed_ms: f64, bytes: usize) {
        let attrs = [
            KeyValue::new("image_type", self.image_type),
            KeyValue::new("result", result),
        ];
        self.read_total.add(1, &attrs);
        self.read_duration.record(elapsed_ms, &attrs);
        if bytes > 0 {
            self.read_bytes.add(bytes as u64, &attrs);
        }
    }
}

#[cfg(test)]
mod tests;
