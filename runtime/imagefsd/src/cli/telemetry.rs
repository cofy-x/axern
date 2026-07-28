use crate::cli::build_metrics_resource;
use crate::log_rotation::RotatingFileWriter;
use anyhow::Result;
use opentelemetry::global;
use opentelemetry::KeyValue;
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::metrics::SdkMeterProvider;
use tokio::runtime::Runtime;

const OTEL_EXPORTER_OTLP_ENDPOINT: &str = "OTEL_EXPORTER_OTLP_ENDPOINT";

pub(super) struct MetricsProvider {
    provider: SdkMeterProvider,
    _runtime: Runtime,
}

impl MetricsProvider {
    pub(super) fn shutdown(self) -> Result<()> {
        let _guard = self._runtime.enter();
        self.provider.force_flush()?;
        self.provider.shutdown()?;
        Ok(())
    }
}

fn metrics_endpoint(explicit_endpoint: &str) -> String {
    if !explicit_endpoint.is_empty() {
        return explicit_endpoint.to_string();
    }
    std::env::var(OTEL_EXPORTER_OTLP_ENDPOINT).unwrap_or_default()
}

pub(super) fn init_metrics(
    otel_endpoint: &str,
    service_name: &str,
    node_id: &str,
    extra_attrs: Vec<KeyValue>,
) -> Result<Option<MetricsProvider>> {
    let otel_endpoint = metrics_endpoint(otel_endpoint);
    if otel_endpoint.is_empty() {
        return Ok(None);
    }

    let runtime = tokio::runtime::Builder::new_multi_thread()
        .worker_threads(1)
        .thread_name("imagefsd-metrics")
        .enable_all()
        .build()?;
    let provider = {
        let _guard = runtime.enter();
        let exporter = opentelemetry_otlp::MetricExporter::builder()
            .with_tonic()
            .with_endpoint(otel_endpoint)
            .build()?;
        SdkMeterProvider::builder()
            .with_periodic_exporter(exporter)
            .with_resource(build_metrics_resource(service_name, node_id, extra_attrs))
            .build()
    };

    global::set_meter_provider(provider.clone());
    Ok(Some(MetricsProvider {
        provider,
        _runtime: runtime,
    }))
}

pub(super) fn init_logging(log_level: tracing::Level, log_file: &str) -> Result<()> {
    if !log_file.is_empty() {
        let file_writer = RotatingFileWriter::new(log_file)?;
        let (non_blocking, guard) = tracing_appender::non_blocking(file_writer);
        Box::leak(Box::new(guard));

        tracing_subscriber::fmt()
            .with_max_level(log_level)
            .with_writer(non_blocking)
            .with_ansi(false)
            .init();
    } else {
        tracing_subscriber::fmt().with_max_level(log_level).init();
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex;

    static ENV_LOCK: Mutex<()> = Mutex::new(());

    #[test]
    fn explicit_metrics_endpoint_takes_precedence() {
        let _guard = ENV_LOCK.lock().unwrap();
        std::env::set_var(OTEL_EXPORTER_OTLP_ENDPOINT, "http://env:4317");
        assert_eq!(
            metrics_endpoint("http://explicit:4317"),
            "http://explicit:4317"
        );
        std::env::remove_var(OTEL_EXPORTER_OTLP_ENDPOINT);
    }

    #[test]
    fn metrics_endpoint_uses_standard_otel_environment() {
        let _guard = ENV_LOCK.lock().unwrap();
        std::env::set_var(OTEL_EXPORTER_OTLP_ENDPOINT, "http://collector:4317");
        assert_eq!(metrics_endpoint(""), "http://collector:4317");
        std::env::remove_var(OTEL_EXPORTER_OTLP_ENDPOINT);
    }

    #[test]
    fn metrics_provider_owns_export_runtime() {
        let _guard = ENV_LOCK.lock().unwrap();
        let provider = init_metrics(
            "http://127.0.0.1:1",
            "imagefsd-test",
            "node-test",
            Vec::new(),
        )
        .unwrap()
        .unwrap();
        let _ = provider.shutdown();
    }
}
