#[cfg(test)]
use crate::backend::peer::default_chunk_server_socket;
use clap::{Args, Parser, Subcommand, ValueEnum};
use opentelemetry::KeyValue;
use opentelemetry_sdk::Resource;
#[cfg(test)]
use std::path::PathBuf;

#[path = "cli/chunk.rs"]
mod chunk;
#[path = "cli/mount.rs"]
mod mount;
#[path = "cli/telemetry.rs"]
mod telemetry;

const DEFAULT_CHUNK_INDEX_TTL_SECS: u64 = 12 * 60 * 60;
const DEFAULT_TOKIO_WORKER_THREADS: usize = 8;
const DEFAULT_NYDUS_DECODED_CACHE_BYTES: usize = 8 * 1024 * 1024;

#[derive(Copy, Clone, Debug, PartialEq, Eq, PartialOrd, Ord, ValueEnum)]
enum Source {
    Local,
    Oss,
    Nydus,
}

impl Source {
    fn as_str(self) -> &'static str {
        match self {
            Self::Local => "local",
            Self::Oss => "oss",
            Self::Nydus => "nydus",
        }
    }
}

fn parse_non_empty_node_id(value: &str) -> Result<String, String> {
    let value = value.trim();
    if value.is_empty() {
        return Err("node ID must not be empty".to_string());
    }
    Ok(value.to_string())
}

#[derive(Args)]
struct FsOptions {
    /// In production, we daemonize the process to ensure it keeps running
    /// even if the controlling process (e.g., a shell or deployment script)
    /// restarts or is upgraded. We also configure a custom log path and a PID file.
    #[arg(long, default_value_t = false)]
    daemon: bool,
    #[arg(long, default_value = "")]
    pid_file: String,
    #[arg(long, default_value = "")]
    log_file: String,
    /// for oss: object name,
    /// for local: local file name
    #[arg(long)]
    name: String,
    /// FUSE mountpoint dir
    #[arg(long)]
    mountpoint: String,
    /// Stable control-plane node identity used by bounded cache metrics.
    #[arg(long, value_parser = parse_non_empty_node_id)]
    node_id: String,
    /// for oss: local cached file path,
    /// for local: local file path
    #[arg(long, default_value = "")]
    cache_file: String,
    /// for nydus: cache directory for blob files
    #[arg(long, default_value = "")]
    cache_dir: String,
    /// for oss: oss config file path,
    /// for local: useless
    /// for nydus: backend config file path
    #[arg(long, default_value = "")]
    cfg: String,
    #[arg(long, value_enum)]
    src: Source,
    #[arg(long, default_value_t = 4)]
    fuse_worker_num: u32,
    #[arg(long, default_value = "")]
    chunk_db_dir: String,
    #[arg(long, default_value = "")]
    image_meta_dir: String,
    /// for nydus: bootstrap file path
    #[arg(long, default_value = "")]
    bootstrap: String,
    #[arg(long, default_value = "")]
    chunk_server_sock: String,
    #[arg(long, default_value_t = 1000)]
    chunk_server_timeout_ms: u64,
    #[arg(long, default_value_t = DEFAULT_TOKIO_WORKER_THREADS)]
    tokio_worker_threads: usize,
    #[arg(long, default_value = "")]
    otel_endpoint: String,
    /// Number of background workers for demand-triggered Nydus cache readahead.
    #[arg(long, default_value_t = 0)]
    nydus_readahead_workers: usize,
    /// Bounded bytes scheduled after a successful foreground Nydus read.
    #[arg(long, default_value_t = 32 * 1024 * 1024)]
    nydus_readahead_window_bytes: usize,
    /// Per-mount byte limit for coalescing and briefly reusing decoded Nydus chunks.
    #[arg(long, default_value_t = DEFAULT_NYDUS_DECODED_CACHE_BYTES)]
    nydus_decoded_cache_bytes: usize,
}

#[derive(Args)]
struct ServeChunkOptions {
    #[arg(long, default_value_t = false)]
    daemon: bool,
    #[arg(long, default_value = "")]
    pid_file: String,
    #[arg(long, default_value = "")]
    log_file: String,
    #[arg(long)]
    chunk_db_dir: String,
    #[arg(long, default_value_t = 9876)]
    listen_port: u16,
    #[arg(long, default_value = "")]
    peer_addrs: String,
    #[arg(long, default_value = "")]
    peer_discovery: String,
    #[arg(long, default_value = "")]
    advertise_addr: String,
    #[arg(long, default_value = "")]
    node_id: String,
    #[arg(long, default_value = "")]
    chunk_server_sock: String,
    #[arg(long, default_value = "")]
    chunk_index_url: String,
    #[arg(long, default_value_t = DEFAULT_CHUNK_INDEX_TTL_SECS)]
    chunk_index_ttl: u64,
    #[arg(long, default_value_t = DEFAULT_TOKIO_WORKER_THREADS)]
    tokio_worker_threads: usize,
    #[arg(long, default_value = "")]
    otel_endpoint: String,
}

#[derive(Args)]
struct GcOptions {
    #[arg(long)]
    chunk_db_dir: String,
    #[arg(long, default_value = "")]
    chunk_server_sock: String,
    #[arg(long, default_value_t = false)]
    dry_run: bool,
}

#[derive(Args)]
struct StatsOptions {
    #[arg(long)]
    chunk_db_dir: String,
}

#[derive(Args)]
struct StatsLocalityOptions {
    #[arg(long)]
    chunk_db_dir: String,
    #[arg(long, default_value = "")]
    chunk_server_sock: String,
}

#[derive(Subcommand)]
enum Commands {
    Mount(Box<FsOptions>),
    ServeChunk(Box<ServeChunkOptions>),
    GcChunk(GcOptions),
    StatsChunk(StatsOptions),
    StatsLocality(StatsLocalityOptions),
}

#[cfg(test)]
fn resolve_chunk_server_socket(chunk_db_dir: &str, chunk_server_sock: &str) -> Option<PathBuf> {
    mount::resolve_chunk_server_socket(chunk_db_dir, chunk_server_sock)
}

#[cfg(test)]
mod tests {
    use super::*;
    use opentelemetry::{Key, Value};

    fn resource_value(resource: &Resource, key: &str) -> Option<Value> {
        resource.get(&Key::new(key.to_string()))
    }

    #[test]
    fn build_metrics_resource_includes_mount_attrs_and_instance_id() {
        let resource = build_metrics_resource(
            "imagefsd-mount",
            "node-a",
            vec![
                KeyValue::new("imagefsd.mountpoint", "/mnt/distill"),
                KeyValue::new("imagefsd.src", "oss"),
                KeyValue::new("service.instance.id", "mount:node-a:oss:/mnt/distill"),
            ],
        );

        assert_eq!(
            resource_value(&resource, "service.name"),
            Some(Value::from("imagefsd-mount"))
        );
        assert_eq!(
            resource_value(&resource, "node.id"),
            Some(Value::from("node-a"))
        );
        assert_eq!(
            resource_value(&resource, "imagefsd.mountpoint"),
            Some(Value::from("/mnt/distill"))
        );
        assert_eq!(
            resource_value(&resource, "imagefsd.src"),
            Some(Value::from("oss"))
        );
        assert_eq!(
            resource_value(&resource, "service.instance.id"),
            Some(Value::from("mount:node-a:oss:/mnt/distill"))
        );
    }

    #[test]
    fn build_metrics_resource_excludes_mount_attrs_for_chunkserver() {
        let resource = build_metrics_resource(
            "imagefsd-chunkserver",
            "node-b",
            vec![KeyValue::new(
                "service.instance.id",
                "chunkserver:node-b@10.0.0.1:9876",
            )],
        );

        assert_eq!(
            resource_value(&resource, "service.instance.id"),
            Some(Value::from("chunkserver:node-b@10.0.0.1:9876"))
        );
        assert_eq!(resource_value(&resource, "imagefsd.mountpoint"), None);
        assert_eq!(resource_value(&resource, "imagefsd.src"), None);
    }

    #[test]
    fn resolve_chunk_server_socket_prefers_explicit_path() {
        let socket = resolve_chunk_server_socket("/tmp/chunkdb", "/tmp/custom.sock");

        assert_eq!(socket, Some(PathBuf::from("/tmp/custom.sock")));
    }

    #[test]
    fn resolve_chunk_server_socket_defaults_to_chunkdb_path() {
        let socket = resolve_chunk_server_socket("/tmp/chunkdb", "");

        assert_eq!(socket, Some(default_chunk_server_socket("/tmp/chunkdb")));
    }

    #[test]
    fn mount_rejects_an_empty_node_id() {
        let result = Cli::try_parse_from([
            "imagefsd",
            "mount",
            "--name",
            "image",
            "--mountpoint",
            "/tmp/image",
            "--node-id",
            "   ",
            "--src",
            "local",
        ]);

        assert!(result.is_err());
    }
}

fn build_metrics_resource(
    service_name: &str,
    node_id: &str,
    extra_attrs: Vec<KeyValue>,
) -> Resource {
    let mut attrs = vec![
        KeyValue::new("service.name", service_name.to_string()),
        KeyValue::new("node.id", node_id.to_string()),
    ];
    attrs.extend(extra_attrs);
    Resource::builder_empty().with_attributes(attrs).build()
}

#[derive(Parser)]
#[command(name = "imagefsd")]
#[command(version = concat!("0.1 (", env!("GIT_COMMIT_HASH"), ")"))]
#[command(about = "dedup, on_demand, readonly fs", long_about = None)]
struct Cli {
    #[arg(short = 'd', long, default_value_t = false)]
    verbose: bool,
    #[command(subcommand)]
    commands: Commands,
}

pub fn run() -> anyhow::Result<()> {
    let cli: Cli = Cli::parse();
    let log_level = if cli.verbose {
        tracing::Level::DEBUG
    } else {
        tracing::Level::INFO
    };

    match &cli.commands {
        Commands::Mount(fs_opts) => mount::run_mount(fs_opts, log_level),
        Commands::ServeChunk(opts) => chunk::run_serve_chunk(opts, log_level),
        Commands::GcChunk(gc_opts) => chunk::run_gc_chunk(gc_opts, log_level),
        Commands::StatsChunk(stats_opts) => chunk::run_stats_chunk(stats_opts, log_level),
        Commands::StatsLocality(stats_opts) => chunk::run_stats_locality(stats_opts, log_level),
    }
}
