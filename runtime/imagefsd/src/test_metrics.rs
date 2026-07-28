use opentelemetry::{metrics::Meter, metrics::MeterProvider as _, KeyValue};
use opentelemetry_sdk::metrics::{
    data::{AggregatedMetrics, Metric, MetricData, ResourceMetrics},
    InMemoryMetricExporter, SdkMeterProvider,
};
use std::collections::BTreeMap;

pub(crate) struct MetricsHarness {
    provider: SdkMeterProvider,
    exporter: InMemoryMetricExporter,
}

impl MetricsHarness {
    pub(crate) fn new() -> Self {
        let exporter = InMemoryMetricExporter::default();
        let provider = SdkMeterProvider::builder()
            .with_periodic_exporter(exporter.clone())
            .build();
        Self { provider, exporter }
    }

    pub(crate) fn meter(&self, name: &'static str) -> Meter {
        self.provider.meter(name)
    }

    pub(crate) fn collect(&self) -> ResourceMetrics {
        self.provider.force_flush().unwrap();
        self.exporter
            .get_finished_metrics()
            .unwrap()
            .into_iter()
            .last()
            .expect("metrics were not exported")
    }
}

pub(crate) fn attrs_to_map<'a>(
    attrs: impl Iterator<Item = &'a KeyValue>,
) -> BTreeMap<String, String> {
    attrs
        .map(|kv| (kv.key.as_str().to_string(), kv.value.to_string()))
        .collect()
}

fn find_metric<'a>(metrics: &'a ResourceMetrics, name: &str) -> &'a Metric {
    metrics
        .scope_metrics()
        .flat_map(|scope| scope.metrics())
        .find(|metric| metric.name() == name)
        .unwrap_or_else(|| panic!("metric {name} not found"))
}

pub(crate) fn sum_points_u64(
    metrics: &ResourceMetrics,
    name: &str,
) -> Vec<(BTreeMap<String, String>, u64)> {
    let metric = find_metric(metrics, name);
    let sum = match metric.data() {
        AggregatedMetrics::U64(MetricData::Sum(sum)) => sum,
        _ => panic!("metric {name} is not Sum<u64>"),
    };
    sum.data_points()
        .map(|point| (attrs_to_map(point.attributes()), point.value()))
        .collect()
}

pub(crate) fn histogram_points_f64(
    metrics: &ResourceMetrics,
    name: &str,
) -> Vec<(BTreeMap<String, String>, u64, f64)> {
    let metric = find_metric(metrics, name);
    let histogram = match metric.data() {
        AggregatedMetrics::F64(MetricData::Histogram(histogram)) => histogram,
        _ => panic!("metric {name} is not Histogram<f64>"),
    };
    histogram
        .data_points()
        .map(|point| (attrs_to_map(point.attributes()), point.count(), point.sum()))
        .collect()
}

#[cfg(target_os = "linux")]
pub(crate) fn gauge_points_u64(
    metrics: &ResourceMetrics,
    name: &str,
) -> Vec<(BTreeMap<String, String>, u64)> {
    let metric = find_metric(metrics, name);
    let gauge = match metric.data() {
        AggregatedMetrics::U64(MetricData::Gauge(gauge)) => gauge,
        _ => panic!("metric {name} is not Gauge<u64>"),
    };
    gauge
        .data_points()
        .map(|point| (attrs_to_map(point.attributes()), point.value()))
        .collect()
}

#[cfg(target_os = "linux")]
pub(crate) fn gauge_points_f64(
    metrics: &ResourceMetrics,
    name: &str,
) -> Vec<(BTreeMap<String, String>, f64)> {
    let metric = find_metric(metrics, name);
    let gauge = match metric.data() {
        AggregatedMetrics::F64(MetricData::Gauge(gauge)) => gauge,
        _ => panic!("metric {name} is not Gauge<f64>"),
    };
    gauge
        .data_points()
        .map(|point| (attrs_to_map(point.attributes()), point.value()))
        .collect()
}
