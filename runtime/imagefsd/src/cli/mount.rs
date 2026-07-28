use super::{FsOptions, Source};
use crate::backend::cache::Cache;
use crate::backend::chunkdb::{ChunkDB, ChunkIndexControl};
use crate::backend::general::GeneralBackend;
use crate::backend::indexdb::IndexDB;
use crate::backend::peer::{default_chunk_server_socket, LocalChunkClient, PeerRuntime};
use crate::backend::{Backend, BackendEx};
use crate::cli::telemetry::{init_logging, init_metrics};
use crate::fs::mount_fs;
use crate::image::nydus::{NydusCacheConfig, NydusImage};
use crate::image::raw::RawImage;
use daemonize::Daemonize;
use opentelemetry::global;
use opentelemetry::metrics::Histogram;
use opentelemetry::KeyValue;
use std::io;
use std::io::ErrorKind;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tracing::{info, warn};

#[derive(Clone)]
struct StartupPhaseMetrics {
    phase_duration: Histogram<f64>,
}

impl StartupPhaseMetrics {
    fn new() -> Self {
        let meter = global::meter("imagefsd.mount");
        Self::with_meter(&meter)
    }

    fn with_meter(meter: &opentelemetry::metrics::Meter) -> Self {
        Self {
            phase_duration: meter
                .f64_histogram("imagefsd.mount.startup_phase_duration_seconds")
                .with_description("Startup-critical Nydus mount phase duration")
                .with_unit("s")
                .build(),
        }
    }

    fn record(
        &self,
        source: &'static str,
        phase: &'static str,
        result: &'static str,
        duration: Duration,
    ) {
        self.phase_duration.record(
            duration.as_secs_f64(),
            &[
                KeyValue::new("canonical_phase", "rootfs_prepare"),
                KeyValue::new("source", source),
                KeyValue::new("phase", phase),
                KeyValue::new("result", result),
            ],
        );
    }

    fn record_result<T, E, F>(
        &self,
        source: &'static str,
        phase: &'static str,
        work: F,
    ) -> Result<T, E>
    where
        F: FnOnce() -> Result<T, E>,
    {
        let started_at = std::time::Instant::now();
        let result = work();
        self.record(
            source,
            phase,
            if result.is_ok() { "ok" } else { "error" },
            started_at.elapsed(),
        );
        result
    }
}

pub(super) fn build_local_chunk_client(opts: &FsOptions) -> Option<Arc<LocalChunkClient>> {
    let socket_path = resolve_chunk_server_socket(&opts.chunk_db_dir, &opts.chunk_server_sock)?;
    let runtime = PeerRuntime::new_with_worker_threads(opts.tokio_worker_threads).ok()?;
    let client = LocalChunkClient::new(
        runtime,
        socket_path,
        Duration::from_millis(opts.chunk_server_timeout_ms),
    );
    client.start_health_checker();
    Some(Arc::new(client))
}

pub(super) fn resolve_chunk_server_socket(
    chunk_db_dir: &str,
    chunk_server_sock: &str,
) -> Option<PathBuf> {
    if !chunk_server_sock.is_empty() {
        Some(PathBuf::from(chunk_server_sock))
    } else if !chunk_db_dir.is_empty() {
        Some(default_chunk_server_socket(chunk_db_dir))
    } else {
        None
    }
}

fn mount_metrics_instance_id(node_id: &str, source: &str, mountpoint: &str) -> String {
    format!("mount:{node_id}:{source}:{mountpoint}")
}

fn setup_chunk_and_index_db(
    chunk_db_dir: &str,
    image_meta_dir: &str,
    index_ctl: Option<Arc<dyn ChunkIndexControl>>,
) -> anyhow::Result<Option<(Arc<ChunkDB>, Arc<IndexDB>)>> {
    let conf_chunk_db = !chunk_db_dir.is_empty();
    let conf_image_meta = !image_meta_dir.is_empty();
    if conf_image_meta != conf_chunk_db {
        return Err(io::Error::new(
            ErrorKind::InvalidInput,
            "Invalid chunk_db_dir & image_meta_dir config",
        )
        .into());
    }
    if conf_chunk_db {
        let chunk_db = Arc::new(ChunkDB::new_with_index_ctl(chunk_db_dir, index_ctl)?);
        let index_db = Arc::new(IndexDB::open(image_meta_dir)?);
        Ok(Some((chunk_db, index_db)))
    } else {
        Ok(None)
    }
}

fn prepare_oss(opts: &FsOptions) -> anyhow::Result<RawImage> {
    let backend = GeneralBackend::new(&opts.cfg)?;
    let oss_file = backend.get_reader(&opts.name)?;
    let size = oss_file.size();
    if size == 0 {
        return Err(io::Error::new(ErrorKind::InvalidInput, "Invalid oss file").into());
    }
    let cached_oss = Cache::new_with_node_id(oss_file, &opts.cache_file, &opts.node_id)?;
    let b = Arc::new(cached_oss);
    let local_chunk_client = build_local_chunk_client(opts);
    let dedup_db = setup_chunk_and_index_db(
        &opts.chunk_db_dir,
        &opts.image_meta_dir,
        local_chunk_client
            .as_ref()
            .map(|client| client.clone() as Arc<dyn ChunkIndexControl>),
    )?;
    RawImage::new(
        &opts.name,
        b as Arc<dyn BackendEx>,
        dedup_db,
        local_chunk_client,
    )
}

fn prepare_local(opts: &FsOptions) -> anyhow::Result<RawImage> {
    let local_file = Cache::from_raw_file(&opts.cache_file, &opts.node_id)?;
    let size = local_file.size();
    if size == 0 {
        return Err(io::Error::new(ErrorKind::InvalidInput, "invalid local file").into());
    }
    let b = Arc::new(local_file);
    let local_chunk_client = build_local_chunk_client(opts);
    let dedup_db = setup_chunk_and_index_db(
        &opts.chunk_db_dir,
        &opts.image_meta_dir,
        local_chunk_client
            .as_ref()
            .map(|client| client.clone() as Arc<dyn ChunkIndexControl>),
    )?;
    RawImage::new(
        &opts.name,
        b as Arc<dyn BackendEx>,
        dedup_db,
        local_chunk_client,
    )
}

pub(super) fn run_mount(fs_opts: &FsOptions, log_level: tracing::Level) -> anyhow::Result<()> {
    if fs_opts.node_id.trim().is_empty() {
        anyhow::bail!("node-id must not be empty");
    }
    if fs_opts.daemon {
        Daemonize::new().pid_file(&fs_opts.pid_file).start()?;
    }
    init_logging(log_level, &fs_opts.log_file)?;
    let src = fs_opts.src.as_str();
    let node_id = &fs_opts.node_id;
    let t = Instant::now();
    let metrics_provider = init_metrics(
        &fs_opts.otel_endpoint,
        "imagefsd-daemon",
        node_id,
        vec![
            KeyValue::new("imagefsd.mountpoint", fs_opts.mountpoint.clone()),
            KeyValue::new("imagefsd.src", src),
            KeyValue::new(
                "service.instance.id",
                mount_metrics_instance_id(node_id, src, &fs_opts.mountpoint),
            ),
        ],
    )?;
    info!("init_metrics took {:?}", t.elapsed());
    let startup_metrics = StartupPhaseMetrics::new();

    let result = match fs_opts.src {
        Source::Local => {
            let image = prepare_local(fs_opts)?;
            mount_fs(image, &fs_opts.mountpoint, fs_opts.fuse_worker_num)
        }
        Source::Oss => {
            let image = prepare_oss(fs_opts)?;
            mount_fs(image, &fs_opts.mountpoint, fs_opts.fuse_worker_num)
        }
        Source::Nydus => {
            let t = std::time::Instant::now();
            let local_chunk_client = build_local_chunk_client(fs_opts);
            let local_chunk_client_elapsed = t.elapsed();
            info!(
                "build_local_chunk_client took {:?}",
                local_chunk_client_elapsed
            );
            startup_metrics.record(
                "nydus",
                "local_chunk_client_setup",
                "ok",
                local_chunk_client_elapsed,
            );

            let chunk_index_started_at = std::time::Instant::now();
            let dedup_db =
                startup_metrics.record_result("nydus", "chunk_index_db_setup", || {
                    setup_chunk_and_index_db(
                        &fs_opts.chunk_db_dir,
                        &fs_opts.image_meta_dir,
                        local_chunk_client
                            .as_ref()
                            .map(|client| client.clone() as Arc<dyn ChunkIndexControl>),
                    )
                })?;
            info!(
                "setup_chunk_and_index_db took {:?}",
                chunk_index_started_at.elapsed()
            );

            let image_open_started_at = std::time::Instant::now();
            let fs = startup_metrics.record_result("nydus", "nydus_image_open", || {
                NydusImage::new(
                    &fs_opts.bootstrap,
                    &fs_opts.cfg,
                    &fs_opts.cache_dir,
                    dedup_db,
                    local_chunk_client,
                    NydusCacheConfig::new(
                        fs_opts.nydus_readahead_workers,
                        fs_opts.nydus_readahead_window_bytes,
                        fs_opts.nydus_decoded_cache_bytes,
                        fs_opts.node_id.clone(),
                    ),
                )
            })?;
            info!("NydusImage::new took {:?}", image_open_started_at.elapsed());

            let mount_start = std::time::Instant::now();
            let mount_result = mount_fs(fs, &fs_opts.mountpoint, fs_opts.fuse_worker_num);
            startup_metrics.record(
                "nydus",
                "filesystem_mount_start",
                if mount_result.is_ok() { "ok" } else { "error" },
                mount_start.elapsed(),
            );
            mount_result
        }
    };
    if let Some(provider) = metrics_provider {
        if let Err(error) = provider.shutdown() {
            warn!(?error, "failed to flush metrics during shutdown");
        }
    }
    result
}

#[cfg(test)]
mod tests {
    use super::{mount_metrics_instance_id, StartupPhaseMetrics};
    use crate::test_metrics::{histogram_points_f64, MetricsHarness};

    #[test]
    fn startup_phase_metrics_emit_expected_labels() {
        let harness = MetricsHarness::new();
        let metrics = StartupPhaseMetrics::with_meter(&harness.meter("imagefsd.mount.test"));

        metrics.record(
            "nydus",
            "local_chunk_client_setup",
            "ok",
            std::time::Duration::from_millis(12),
        );
        metrics.record(
            "nydus",
            "filesystem_mount_start",
            "error",
            std::time::Duration::from_millis(7),
        );
        let _: Result<(), std::io::Error> =
            metrics.record_result("nydus", "nydus_image_open", || {
                Err(std::io::Error::other("boom"))
            });

        let points = histogram_points_f64(
            &harness.collect(),
            "imagefsd.mount.startup_phase_duration_seconds",
        );
        if points.len() != 3 {
            panic!("startup phase points = {}, want 3", points.len());
        }

        let mut saw_error = false;
        for (attrs, _, _) in points {
            assert_eq!(
                attrs.get("canonical_phase").map(String::as_str),
                Some("rootfs_prepare")
            );
            assert_eq!(attrs.get("source").map(String::as_str), Some("nydus"));
            if attrs.get("phase").map(String::as_str) == Some("nydus_image_open")
                && attrs.get("result").map(String::as_str) == Some("error")
            {
                saw_error = true;
            }
        }
        assert!(saw_error, "expected error sample for nydus_image_open");
    }

    #[test]
    fn mount_metrics_instance_id_is_unique_per_node() {
        assert_eq!(
            mount_metrics_instance_id("node-a", "nydus", "/mnt/image"),
            "mount:node-a:nydus:/mnt/image"
        );
        assert_ne!(
            mount_metrics_instance_id("node-a", "nydus", "/mnt/image"),
            mount_metrics_instance_id("node-b", "nydus", "/mnt/image")
        );
    }
}
