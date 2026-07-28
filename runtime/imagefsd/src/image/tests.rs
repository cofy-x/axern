use super::*;
use crate::test_metrics::{histogram_points_f64, sum_points_u64, MetricsHarness};
use std::collections::BTreeMap;

fn attrs(pairs: &[(&str, &str)]) -> BTreeMap<String, String> {
    pairs
        .iter()
        .map(|(key, value)| ((*key).to_string(), (*value).to_string()))
        .collect()
}

#[test]
fn fs_read_metrics_emit_raw_labels() {
    let harness = MetricsHarness::new();
    let metrics = FsReadMetrics::with_meter(&harness.meter("imagefsd.fs.test"), "raw");

    metrics.record_read("ok", 12.5, 4096);

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.fs.read_total")
        .contains(&(attrs(&[("image_type", "raw"), ("result", "ok")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.fs.read_bytes")
        .contains(&(attrs(&[("image_type", "raw"), ("result", "ok")]), 4096)));
    assert!(
        histogram_points_f64(&collected, "imagefsd.fs.read_duration_ms").contains(&(
            attrs(&[("image_type", "raw"), ("result", "ok")]),
            1,
            12.5
        ))
    );
}

#[test]
fn fs_read_metrics_emit_nydus_labels() {
    let harness = MetricsHarness::new();
    let metrics = FsReadMetrics::with_meter(&harness.meter("imagefsd.fs.test"), "nydus");

    metrics.record_read("error", 7.0, 0);

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.fs.read_total")
        .contains(&(attrs(&[("image_type", "nydus"), ("result", "error")]), 1)));
    assert!(
        histogram_points_f64(&collected, "imagefsd.fs.read_duration_ms").contains(&(
            attrs(&[("image_type", "nydus"), ("result", "error")]),
            1,
            7.0
        ))
    );
}
