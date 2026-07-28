mod accept;
mod handler;
mod index_sync;
mod requests;

use super::client::PeerClient;
use super::metrics::ChunkServerMetrics;
use super::{PeerRuntime, ShutdownHandle};
use crate::backend::chunkdb::ChunkDB;
use anyhow::Context;
use opentelemetry::global;
use std::fs;
use std::io;
use std::net::{SocketAddr, TcpListener as StdTcpListener};
use std::os::unix::net::UnixListener as StdUnixListener;
use std::path::{Path, PathBuf};
use std::process;
use std::sync::Arc;
use std::time::Duration;
use tokio::net::{TcpListener, UnixListener};
use tokio::sync::Semaphore;
use tokio::task::JoinHandle;

use self::accept::ConnectionAcceptor;
use self::index_sync::IndexMaintainer;

#[cfg(all(test, target_os = "linux"))]
use super::index::ChunkIndex;

pub struct ChunkServer {
    runtime: PeerRuntime,
    chunk_db: Arc<ChunkDB>,
    pub(super) tcp_listener: StdTcpListener,
    unix_listener: StdUnixListener,
    unix_socket_path: PathBuf,
    shutdown: ShutdownHandle,
    pub(super) peer_client: Option<Arc<PeerClient>>,
    max_connections: usize,
    pub(super) metrics: Arc<ChunkServerMetrics>,
}

impl std::fmt::Debug for ChunkServer {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ChunkServer").finish()
    }
}

impl ChunkServer {
    pub fn new<P: AsRef<Path>>(
        runtime: PeerRuntime,
        chunk_db: Arc<ChunkDB>,
        listen_addr: SocketAddr,
        unix_socket_path: P,
        peer_client: Option<Arc<PeerClient>>,
    ) -> anyhow::Result<Self> {
        let unix_socket_path = unix_socket_path.as_ref().to_path_buf();
        if let Some(parent) = unix_socket_path.parent() {
            fs::create_dir_all(parent)?;
        }
        if unix_socket_path.exists() {
            fs::remove_file(&unix_socket_path)?;
        }

        let tcp_listener = StdTcpListener::bind(listen_addr)
            .with_context(|| format!("failed to bind tcp listener on {listen_addr}"))?;
        let unix_listener = StdUnixListener::bind(&unix_socket_path).with_context(|| {
            format!(
                "failed to bind unix listener on {}",
                unix_socket_path.display()
            )
        })?;
        tcp_listener.set_nonblocking(true)?;
        unix_listener.set_nonblocking(true)?;

        let meter = global::meter("imagefsd.chunkserver");
        Ok(Self {
            runtime,
            chunk_db,
            tcp_listener,
            unix_listener,
            unix_socket_path,
            shutdown: ShutdownHandle::new(),
            peer_client,
            max_connections: super::DEFAULT_MAX_CONNECTIONS,
            metrics: Arc::new(ChunkServerMetrics::new(&meter)),
        })
    }

    #[allow(dead_code)]
    pub fn shutdown_handle(&self) -> ShutdownHandle {
        self.shutdown.clone()
    }

    #[allow(dead_code)]
    pub fn local_addr(&self) -> io::Result<SocketAddr> {
        self.tcp_listener.local_addr()
    }

    pub fn run(self) -> anyhow::Result<()> {
        let runtime = self.runtime.clone();
        runtime.block_on(self.run_inner())
    }

    async fn run_inner(self) -> anyhow::Result<()> {
        let shutdown = self.shutdown.clone();
        let tcp_listener = TcpListener::from_std(self.tcp_listener.try_clone()?)?;
        let unix_listener = UnixListener::from_std(self.unix_listener.try_clone()?)?;
        let semaphore = Arc::new(Semaphore::new(self.max_connections));
        self.run_index_sync();

        let gc_task = self
            .peer_client
            .as_ref()
            .map(|client| client.start_background_tasks(shutdown.clone()));
        let index_refresh_task = self.run_index_refresh(shutdown.clone());

        let stale_chunk_db = Arc::clone(&self.chunk_db);
        let stale_shutdown = shutdown.clone();
        tokio::spawn(async move {
            let mut interval = tokio::time::interval(Duration::from_secs(60));
            interval.tick().await;
            loop {
                interval.tick().await;
                if stale_shutdown.is_shutdown() {
                    break;
                }
                let _ = stale_chunk_db.clear_stale_readers();
            }
        });

        let tcp_accept = ConnectionAcceptor::new(
            Arc::clone(&self.chunk_db),
            self.peer_client.clone(),
            Arc::clone(&self.metrics),
            Arc::clone(&semaphore),
            shutdown.clone(),
        );
        let unix_accept = ConnectionAcceptor::new(
            Arc::clone(&self.chunk_db),
            self.peer_client.clone(),
            Arc::clone(&self.metrics),
            semaphore,
            shutdown.clone(),
        );
        let tcp_task = tokio::spawn(tcp_accept.serve_tcp(tcp_listener));
        let unix_task = tokio::spawn(unix_accept.serve_unix(unix_listener));

        let tcp_res = tcp_task
            .await
            .map_err(|_| io::Error::other("tcp accept task panicked"))?;
        let unix_res = unix_task
            .await
            .map_err(|_| io::Error::other("unix accept task panicked"))?;
        if let Some(task) = gc_task {
            shutdown.shutdown();
            let _ = task.await;
        }
        if let Some(task) = index_refresh_task {
            let _ = task.await;
        }
        let _ = fs::remove_file(&self.unix_socket_path);
        tcp_res?;
        unix_res?;
        Ok(())
    }

    #[cfg(all(test, target_os = "linux"))]
    pub(super) async fn refresh_index(
        chunk_db: Arc<ChunkDB>,
        chunk_index: Arc<dyn ChunkIndex>,
        spread_over: Duration,
    ) -> anyhow::Result<usize> {
        IndexMaintainer::new(chunk_db, chunk_index)
            .refresh(spread_over)
            .await
    }

    fn run_index_sync(&self) {
        let Some(peer_client) = &self.peer_client else {
            return;
        };
        let Some(chunk_index) = peer_client.chunk_index.clone() else {
            return;
        };
        let maintainer = Arc::new(IndexMaintainer::new(
            Arc::clone(&self.chunk_db),
            chunk_index,
        ));
        self.runtime.spawn(maintainer.sync());
    }

    fn run_index_refresh(&self, shutdown: ShutdownHandle) -> Option<JoinHandle<()>> {
        let peer_client = self.peer_client.as_ref()?;
        let chunk_index = peer_client.chunk_index.clone()?;
        let refresh_interval = chunk_index.refresh_interval()?;
        let maintainer = Arc::new(IndexMaintainer::new(
            Arc::clone(&self.chunk_db),
            chunk_index,
        ));
        Some(
            self.runtime
                .spawn(maintainer.run_refresh_loop(refresh_interval, shutdown)),
        )
    }
}

impl Drop for ChunkServer {
    fn drop(&mut self) {
        self.shutdown.shutdown();
        if let Some(peer_client) = &self.peer_client {
            peer_client.discovery.shutdown();
        }
        let _ = fs::remove_file(&self.unix_socket_path);
    }
}

pub fn default_chunk_server_socket<P: AsRef<Path>>(chunk_db_dir: P) -> PathBuf {
    chunk_db_dir.as_ref().join("chunkserver.sock")
}

pub fn default_node_id() -> String {
    std::env::var("HOSTNAME").unwrap_or_else(|_| format!("imagefsd-{}", process::id()))
}
