use opentelemetry::metrics::{Counter, Histogram, Meter, UpDownCounter};
use opentelemetry::KeyValue;

#[derive(Debug, Clone)]
pub(super) struct ChunkServerMetrics {
    requests_total: Counter<u64>,
    request_duration: Histogram<f64>,
    response_bytes: Counter<u64>,
    active_connections: UpDownCounter<i64>,
}

impl ChunkServerMetrics {
    pub(super) fn new(meter: &Meter) -> Self {
        Self {
            requests_total: meter
                .u64_counter("imagefsd.server.requests_total")
                .with_description("Total requests received")
                .init(),
            request_duration: meter
                .f64_histogram("imagefsd.server.request_duration_ms")
                .with_description("Request processing duration in ms")
                .with_unit("ms")
                .init(),
            response_bytes: meter
                .u64_counter("imagefsd.server.response_bytes")
                .with_description("Total bytes sent in GET_CHUNK responses")
                .with_unit("By")
                .init(),
            active_connections: meter
                .i64_up_down_counter("imagefsd.server.active_connections")
                .with_description("Current active connections")
                .init(),
        }
    }

    pub(super) fn record_connection_delta(&self, transport: &'static str, delta: i64) {
        self.active_connections
            .add(delta, &[KeyValue::new("transport", transport)]);
    }

    pub(super) fn record_request(
        &self,
        request_type: &'static str,
        status: &'static str,
        transport: &'static str,
        elapsed_ms: f64,
        payload_len: usize,
    ) {
        let attrs = [
            KeyValue::new("type", request_type),
            KeyValue::new("status", status),
            KeyValue::new("transport", transport),
        ];
        self.requests_total.add(1, &attrs);
        self.request_duration.record(elapsed_ms, &attrs);
        if payload_len > 0 {
            self.response_bytes.add(payload_len as u64, &attrs);
        }
    }
}

#[derive(Debug, Clone)]
pub(super) struct PeerClientMetrics {
    fetch_total: Counter<u64>,
    fetch_duration: Histogram<f64>,
    query_total: Counter<u64>,
    query_duration: Histogram<f64>,
    retry_total: Counter<u64>,
}

impl PeerClientMetrics {
    pub(super) fn new(meter: &Meter) -> Self {
        Self {
            fetch_total: meter
                .u64_counter("imagefsd.peer.fetch_total")
                .with_description("Total chunk fetch attempts by source")
                .init(),
            fetch_duration: meter
                .f64_histogram("imagefsd.peer.fetch_duration_ms")
                .with_description("End-to-end fetch duration including retries")
                .with_unit("ms")
                .init(),
            query_total: meter
                .u64_counter("imagefsd.peer.query_total")
                .with_description("Total individual peer queries")
                .init(),
            query_duration: meter
                .f64_histogram("imagefsd.peer.query_duration_ms")
                .with_description("Single peer TCP query duration")
                .with_unit("ms")
                .init(),
            retry_total: meter
                .u64_counter("imagefsd.peer.retry_total")
                .with_description("Total retries in index mode")
                .init(),
        }
    }

    pub(super) fn record_fetch(&self, source: &'static str, result: &'static str, elapsed_ms: f64) {
        let attrs = [
            KeyValue::new("source", source),
            KeyValue::new("result", result),
        ];
        self.fetch_total.add(1, &attrs);
        self.fetch_duration.record(elapsed_ms, &attrs);
    }

    pub(super) fn record_query(&self, result: &'static str, elapsed_ms: f64) {
        let attrs = [KeyValue::new("result", result)];
        self.query_total.add(1, &attrs);
        self.query_duration.record(elapsed_ms, &attrs);
    }

    pub(super) fn record_retry(&self, source: &'static str) {
        self.retry_total.add(1, &[KeyValue::new("source", source)]);
    }
}

#[derive(Debug, Clone)]
pub(super) struct ChunkIndexMetrics {
    lookup_total: Counter<u64>,
    lookup_duration: Histogram<f64>,
    register_total: Counter<u64>,
    register_success_total: Counter<u64>,
    unregister_total: Counter<u64>,
    refresh_total: Counter<u64>,
    repair_total: Counter<u64>,
    error_total: Counter<u64>,
    candidates_count: Histogram<f64>,
}

impl ChunkIndexMetrics {
    pub(super) fn new(meter: &Meter) -> Self {
        Self {
            lookup_total: meter
                .u64_counter("imagefsd.index.lookup_total")
                .with_description("Total index lookups")
                .init(),
            lookup_duration: meter
                .f64_histogram("imagefsd.index.lookup_duration_ms")
                .with_description("Index lookup duration (Redis RTT)")
                .with_unit("ms")
                .init(),
            register_total: meter
                .u64_counter("imagefsd.index.register_total")
                .with_description("Total attempted index registrations")
                .init(),
            register_success_total: meter
                .u64_counter("imagefsd.index.register_success_total")
                .with_description("Total successful index registrations")
                .init(),
            unregister_total: meter
                .u64_counter("imagefsd.index.unregister_total")
                .with_description("Total successful index unregisters")
                .init(),
            refresh_total: meter
                .u64_counter("imagefsd.index.refresh_total")
                .with_description("Total chunks refreshed in the index")
                .init(),
            repair_total: meter
                .u64_counter("imagefsd.index.repair_total")
                .with_description("Total chunks repaired in the index")
                .init(),
            error_total: meter
                .u64_counter("imagefsd.index.error_total")
                .with_description("Total index operation errors")
                .init(),
            candidates_count: meter
                .f64_histogram("imagefsd.index.candidates_count")
                .with_description("Number of candidate nodes per lookup")
                .init(),
        }
    }

    pub(super) fn record_lookup(&self, result: &'static str, elapsed_ms: f64, candidates: usize) {
        let attrs = [KeyValue::new("result", result)];
        self.lookup_total.add(1, &attrs);
        self.lookup_duration.record(elapsed_ms, &attrs);
        self.candidates_count.record(candidates as f64, &attrs);
    }

    pub(super) fn record_register_attempt(&self, mode: &'static str, count: u64) {
        self.register_total
            .add(count, &[KeyValue::new("mode", mode)]);
    }

    pub(super) fn record_register_success(&self, mode: &'static str, count: u64) {
        if count > 0 {
            self.register_success_total
                .add(count, &[KeyValue::new("mode", mode)]);
        }
    }

    pub(super) fn record_unregister(&self, mode: &'static str, count: u64) {
        if count > 0 {
            self.unregister_total
                .add(count, &[KeyValue::new("mode", mode)]);
        }
    }

    pub(super) fn record_refresh(&self, result: &'static str, count: u64) {
        if count > 0 {
            self.refresh_total
                .add(count, &[KeyValue::new("result", result)]);
        }
    }

    pub(super) fn record_repair(&self, result: &'static str, count: u64) {
        if count > 0 {
            self.repair_total
                .add(count, &[KeyValue::new("result", result)]);
        }
    }

    pub(super) fn record_error(&self, op: &'static str) {
        self.error_total.add(1, &[KeyValue::new("op", op)]);
    }
}

#[derive(Debug, Clone)]
pub(super) struct LocalClientMetrics {
    requests_total: Counter<u64>,
    request_duration: Histogram<f64>,
}

impl LocalClientMetrics {
    pub(super) fn new(meter: &Meter) -> Self {
        Self {
            requests_total: meter
                .u64_counter("imagefsd.local.request_total")
                .with_description("Total local control requests")
                .init(),
            request_duration: meter
                .f64_histogram("imagefsd.local.request_duration_ms")
                .with_description("Local control request duration")
                .with_unit("ms")
                .init(),
        }
    }

    pub(super) fn record_request(&self, op: &'static str, result: &'static str, elapsed_ms: f64) {
        let attrs = [KeyValue::new("op", op), KeyValue::new("result", result)];
        self.requests_total.add(1, &attrs);
        self.request_duration.record(elapsed_ms, &attrs);
    }
}
