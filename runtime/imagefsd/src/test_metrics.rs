use opentelemetry::{
    metrics::{Meter, MeterProvider as _, Result},
    KeyValue,
};
use opentelemetry_sdk::{
    metrics::{
        data::{Histogram, Metric, ResourceMetrics, Sum},
        reader::{AggregationSelector, MetricReader, TemporalitySelector},
        Aggregation, InstrumentKind, ManualReader, Pipeline, SdkMeterProvider,
    },
    Resource,
};
use std::collections::BTreeMap;
use std::sync::{Arc, Weak};

#[derive(Clone, Debug)]
struct SharedReader(Arc<dyn MetricReader>);

impl TemporalitySelector for SharedReader {
    fn temporality(&self, kind: InstrumentKind) -> opentelemetry_sdk::metrics::data::Temporality {
        self.0.temporality(kind)
    }
}

impl AggregationSelector for SharedReader {
    fn aggregation(&self, kind: InstrumentKind) -> Aggregation {
        self.0.aggregation(kind)
    }
}

impl MetricReader for SharedReader {
    fn register_pipeline(&self, pipeline: Weak<Pipeline>) {
        self.0.register_pipeline(pipeline);
    }

    fn collect(&self, rm: &mut ResourceMetrics) -> Result<()> {
        self.0.collect(rm)
    }

    fn force_flush(&self) -> Result<()> {
        self.0.force_flush()
    }

    fn shutdown(&self) -> Result<()> {
        self.0.shutdown()
    }
}

pub(crate) struct MetricsHarness {
    _provider: SdkMeterProvider,
    reader: SharedReader,
}

impl MetricsHarness {
    pub(crate) fn new() -> Self {
        let reader = SharedReader(Arc::new(ManualReader::builder().build()));
        let provider = SdkMeterProvider::builder()
            .with_reader(reader.clone())
            .build();
        Self {
            _provider: provider,
            reader,
        }
    }

    pub(crate) fn meter(&self, name: &'static str) -> Meter {
        self._provider.meter(name)
    }

    pub(crate) fn collect(&self) -> ResourceMetrics {
        let mut metrics = ResourceMetrics {
            resource: Resource::empty(),
            scope_metrics: Vec::new(),
        };
        self.reader.collect(&mut metrics).unwrap();
        metrics
    }
}

pub(crate) fn attrs_to_map(attrs: &[KeyValue]) -> BTreeMap<String, String> {
    attrs
        .iter()
        .map(|kv| (kv.key.as_str().to_string(), kv.value.to_string()))
        .collect()
}

fn find_metric<'a>(metrics: &'a ResourceMetrics, name: &str) -> &'a Metric {
    metrics
        .scope_metrics
        .iter()
        .flat_map(|scope| scope.metrics.iter())
        .find(|metric| metric.name.as_ref() == name)
        .unwrap_or_else(|| panic!("metric {name} not found"))
}

pub(crate) fn sum_points_u64(
    metrics: &ResourceMetrics,
    name: &str,
) -> Vec<(BTreeMap<String, String>, u64)> {
    let metric = find_metric(metrics, name);
    let sum = metric
        .data
        .as_any()
        .downcast_ref::<Sum<u64>>()
        .unwrap_or_else(|| panic!("metric {name} is not Sum<u64>"));
    sum.data_points
        .iter()
        .map(|point| (attrs_to_map(&point.attributes), point.value))
        .collect()
}

pub(crate) fn histogram_points_f64(
    metrics: &ResourceMetrics,
    name: &str,
) -> Vec<(BTreeMap<String, String>, u64, f64)> {
    let metric = find_metric(metrics, name);
    let histogram = metric
        .data
        .as_any()
        .downcast_ref::<Histogram<f64>>()
        .unwrap_or_else(|| panic!("metric {name} is not Histogram<f64>"));
    histogram
        .data_points
        .iter()
        .map(|point| (attrs_to_map(&point.attributes), point.count, point.sum))
        .collect()
}
