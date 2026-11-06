use super::{GcOptions, ServeChunkOptions, StatsLocalityOptions, StatsOptions};
use crate::backend::chunkdb::{ChunkDB, ChunkIndexControl, GcWorker};
use crate::backend::peer::{
    build_discovery, default_chunk_server_socket, default_node_id, ChunkIndex, ChunkServer,
    LocalChunkClient, PeerClient, PeerRuntime, RedisChunkIndex, SyncChunkIndexControl,
};
use crate::cli::mount::resolve_chunk_server_socket;
use crate::cli::telemetry::{init_logging, init_metrics};
use daemonize::Daemonize;
use opentelemetry::KeyValue;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

fn build_gc_local_chunk_client(
    chunk_db_dir: &str,
    chunk_server_sock: &str,
) -> Option<Arc<dyn ChunkIndexControl>> {
    let runtime = PeerRuntime::new().ok()?;
    let socket_path = resolve_chunk_server_socket(chunk_db_dir, chunk_server_sock)?;
    Some(Arc::new(LocalChunkClient::new(
        runtime,
        socket_path,
        Duration::from_millis(1000),
    )))
}

pub(super) fn run_serve_chunk(
    opts: &ServeChunkOptions,
    log_level: tracing::Level,
) -> anyhow::Result<()> {
    if opts.daemon {
        Daemonize::new().pid_file(&opts.pid_file).start()?;
    }
    init_logging(log_level, &opts.log_file)?;
    let runtime = PeerRuntime::new_with_worker_threads(opts.tokio_worker_threads)?;
    let listen_addr: std::net::SocketAddr = format!("0.0.0.0:{}", opts.listen_port).parse()?;
    let advertise_addr: std::net::SocketAddr = if opts.advertise_addr.is_empty() {
        format!("127.0.0.1:{}", opts.listen_port).parse()?
    } else {
        opts.advertise_addr.parse()?
    };
    let node_id = if opts.node_id.is_empty() {
        default_node_id()
    } else {
        opts.node_id.clone()
    };
    let metrics_provider = init_metrics(
        &opts.otel_endpoint,
        "imagefsd-chunkserver",
        &node_id,
        vec![KeyValue::new(
            "service.instance.id",
            format!("chunkserver:{node_id}@{advertise_addr}"),
        )],
    )?;
    let socket_path = if !opts.chunk_server_sock.is_empty() {
        PathBuf::from(&opts.chunk_server_sock)
    } else {
        default_chunk_server_socket(&opts.chunk_db_dir)
    };
    let discovery = build_discovery(
        runtime.clone(),
        &opts.peer_discovery,
        &opts.peer_addrs,
        advertise_addr,
        &node_id,
    )?;
    let chunk_index: Option<Arc<dyn ChunkIndex>> = if opts.chunk_index_url.is_empty() {
        None
    } else {
        Some(Arc::new(RedisChunkIndex::new(
            &opts.chunk_index_url,
            advertise_addr,
            &node_id,
            opts.chunk_index_ttl,
        )?))
    };
    let index_ctl = chunk_index.as_ref().map(|chunk_index| {
        Arc::new(SyncChunkIndexControl::new(
            runtime.clone(),
            chunk_index.clone(),
        )) as Arc<dyn ChunkIndexControl>
    });
    let chunk_db = Arc::new(ChunkDB::new_with_index_ctl(&opts.chunk_db_dir, index_ctl)?);
    let peer_client = if discovery.get_peers().is_empty() && opts.peer_discovery.trim().is_empty() {
        None
    } else {
        let mut client =
            PeerClient::new(runtime.clone(), discovery).with_local_addr(advertise_addr);
        if let Some(chunk_index) = chunk_index {
            client = client.with_chunk_index(chunk_index);
        }
        Some(Arc::new(client))
    };
    let server = ChunkServer::new(runtime, chunk_db, listen_addr, &socket_path, peer_client)?;
    let result = server.run();
    if let Some(provider) = metrics_provider {
        if let Err(error) = provider.shutdown() {
            tracing::warn!(?error, "failed to flush metrics during shutdown");
        }
    }
    result
}

pub(super) fn run_gc_chunk(gc_opts: &GcOptions, log_level: tracing::Level) -> anyhow::Result<()> {
    tracing_subscriber::fmt().with_max_level(log_level).init();

    let worker = GcWorker::new_with_local_client(
        &gc_opts.chunk_db_dir,
        build_gc_local_chunk_client(&gc_opts.chunk_db_dir, &gc_opts.chunk_server_sock),
    )?;
    worker.run(gc_opts.dry_run)
}

pub(super) fn run_stats_chunk(
    stats_opts: &StatsOptions,
    log_level: tracing::Level,
) -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_max_level(log_level)
        .with_writer(std::io::stderr)
        .init();

    let chunk_db = ChunkDB::new(&stats_opts.chunk_db_dir)?;
    let stats = chunk_db.get_stats()?;
    println!("{}", serde_json::to_string_pretty(&stats)?);
    Ok(())
}

pub(super) fn run_stats_locality(
    stats_opts: &StatsLocalityOptions,
    log_level: tracing::Level,
) -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_max_level(log_level)
        .with_writer(std::io::stderr)
        .init();

    let runtime = PeerRuntime::new()?;
    let socket_path =
        resolve_chunk_server_socket(&stats_opts.chunk_db_dir, &stats_opts.chunk_server_sock)
            .ok_or_else(|| anyhow::anyhow!("chunk server socket path is required"))?;
    let client = LocalChunkClient::new(runtime, socket_path, Duration::from_millis(1000));
    let stats = client.stats_locality_blocking()?;
    println!("{}", serde_json::to_string_pretty(&stats)?);
    Ok(())
}
