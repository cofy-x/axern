use super::handler::RequestHandler;
use crate::backend::chunkdb::ChunkDB;
use crate::backend::peer::client::PeerClient;
use crate::backend::peer::metrics::ChunkServerMetrics;
use crate::backend::peer::session::RequestStream;
use crate::backend::peer::ShutdownHandle;
use std::io;
use std::sync::Arc;
use tokio::net::{TcpListener, TcpStream, UnixListener, UnixStream};
use tokio::sync::Semaphore;
use tracing::debug;

pub(super) struct ConnectionAcceptor {
    chunk_db: Arc<ChunkDB>,
    peer_client: Option<Arc<PeerClient>>,
    metrics: Arc<ChunkServerMetrics>,
    semaphore: Arc<Semaphore>,
    shutdown: ShutdownHandle,
}

impl ConnectionAcceptor {
    pub(super) fn new(
        chunk_db: Arc<ChunkDB>,
        peer_client: Option<Arc<PeerClient>>,
        metrics: Arc<ChunkServerMetrics>,
        semaphore: Arc<Semaphore>,
        shutdown: ShutdownHandle,
    ) -> Self {
        Self {
            chunk_db,
            peer_client,
            metrics,
            semaphore,
            shutdown,
        }
    }

    pub(super) async fn serve_tcp(self, listener: TcpListener) -> io::Result<()> {
        self.accept_loop("tcp", listener, |stream| {
            let _ = stream.set_nodelay(true);
            stream
        })
        .await
    }

    pub(super) async fn serve_unix(self, listener: UnixListener) -> io::Result<()> {
        self.accept_loop("unix", listener, |stream| stream).await
    }

    async fn accept_loop<L, S, F>(
        &self,
        transport: &'static str,
        listener: L,
        prepare: F,
    ) -> io::Result<()>
    where
        L: Listener<Stream = S>,
        S: RequestStream,
        F: Fn(S) -> S,
    {
        loop {
            tokio::select! {
                _ = self.shutdown.wait() => break,
                accepted = listener.accept_stream() => {
                    let stream = prepare(accepted?);
                    let permit = self.semaphore.clone().acquire_owned().await.map_err(|_| io::Error::other("semaphore closed"))?;
                    let request_handler = RequestHandler::new(
                        Arc::clone(&self.chunk_db),
                        self.peer_client.clone(),
                        Arc::clone(&self.metrics),
                        transport,
                    );
                    let metrics = Arc::clone(&self.metrics);
                    tokio::spawn(async move {
                        let _permit = permit;
                        metrics.record_connection_delta(transport, 1);
                        if let Err(err) = request_handler.handle(stream).await {
                            debug!(err = debug(err), "{transport} request failed");
                        }
                        metrics.record_connection_delta(transport, -1);
                    });
                }
            }
        }
        Ok(())
    }
}

trait Listener {
    type Stream;
    async fn accept_stream(&self) -> io::Result<Self::Stream>;
}

impl Listener for TcpListener {
    type Stream = TcpStream;

    async fn accept_stream(&self) -> io::Result<TcpStream> {
        self.accept().await.map(|(stream, _)| stream)
    }
}

impl Listener for UnixListener {
    type Stream = UnixStream;

    async fn accept_stream(&self) -> io::Result<UnixStream> {
        self.accept().await.map(|(stream, _)| stream)
    }
}
