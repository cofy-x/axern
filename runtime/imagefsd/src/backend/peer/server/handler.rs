use super::requests::{
    ControlBatchRequest, ControlRequest, GetRequest, HealthCheckRequest, PrefetchRequest,
    StatsLocalityRequest,
};
use crate::backend::chunkdb::{CheckSum, ChunkDB};
use crate::backend::peer::client::PeerClient;
use crate::backend::peer::metrics::ChunkServerMetrics;
use crate::backend::peer::protocol::{read_checksum_batch, MessageType, Request, WireResponse};
use crate::backend::peer::session::RequestStream;
use std::io::{self, ErrorKind};
use std::sync::Arc;
use std::time::Duration;
use tokio::runtime::Handle;
use tokio::sync::mpsc;
use tokio::task::block_in_place;
use tokio::time::timeout;
use tracing::debug;

#[derive(Debug)]
pub(super) enum ResponseAction {
    Immediate(WireResponse),
    GetChunk { request_id: u64, checksum: CheckSum },
}

#[derive(Debug)]
pub(super) struct InboundRequest {
    pub(super) request: Request,
    pub(super) checksums: Option<Vec<CheckSum>>,
}

pub(super) struct RequestContext<'a> {
    pub(super) chunk_db: &'a ChunkDB,
    pub(super) peer_client: Option<&'a PeerClient>,
    pub(super) metrics: &'a ChunkServerMetrics,
    pub(super) transport: &'static str,
}

pub(super) struct RequestHandler {
    chunk_db: Arc<ChunkDB>,
    peer_client: Option<Arc<PeerClient>>,
    metrics: Arc<ChunkServerMetrics>,
    transport: &'static str,
}

impl RequestHandler {
    pub(super) fn new(
        chunk_db: Arc<ChunkDB>,
        peer_client: Option<Arc<PeerClient>>,
        metrics: Arc<ChunkServerMetrics>,
        transport: &'static str,
    ) -> Self {
        Self {
            chunk_db,
            peer_client,
            metrics,
            transport,
        }
    }

    pub(super) async fn handle<S>(self, stream: S) -> io::Result<()>
    where
        S: RequestStream,
    {
        let (mut reader, mut writer) = stream.into_request_parts();
        let (response_tx, mut response_rx) = mpsc::unbounded_channel::<ResponseAction>();
        let chunk_db = Arc::clone(&self.chunk_db);
        let metrics = Arc::clone(&self.metrics);
        let transport = self.transport;
        let writer_task = tokio::spawn(async move {
            while let Some(action) = response_rx.recv().await {
                match action {
                    ResponseAction::Immediate(response) => {
                        writer.write_response(&response).await?;
                    }
                    ResponseAction::GetChunk {
                        request_id,
                        checksum,
                    } => {
                        let begin = std::time::Instant::now();
                        let result = block_in_place(|| {
                            chunk_db.with_chunk(&checksum, |data| {
                                Handle::current()
                                    .block_on(writer.write_get_chunk_hit(request_id, data))?;
                                Ok(data.len())
                            })
                        });
                        let elapsed_ms = begin.elapsed().as_secs_f64() * 1000.0;
                        match result {
                            Ok(Some(payload_len)) => metrics.record_request(
                                "get_chunk",
                                "hit",
                                transport,
                                elapsed_ms,
                                payload_len,
                            ),
                            Ok(None) => {
                                writer
                                    .write_response(&WireResponse {
                                        request_id,
                                        status: super::super::STATUS_MISS,
                                        payload: Vec::new(),
                                    })
                                    .await?;
                                metrics.record_request(
                                    "get_chunk",
                                    "miss",
                                    transport,
                                    elapsed_ms,
                                    0,
                                );
                            }
                            Err(err) => {
                                debug!(err = debug(&err), "get chunk failed");
                                writer
                                    .write_response(&WireResponse {
                                        request_id,
                                        status: super::super::STATUS_ERROR,
                                        payload: Vec::new(),
                                    })
                                    .await?;
                                metrics.record_request(
                                    "get_chunk",
                                    "error",
                                    transport,
                                    elapsed_ms,
                                    0,
                                );
                            }
                        }
                    }
                }
            }
            Ok::<(), io::Error>(())
        });

        loop {
            let request = match timeout(
                Duration::from_secs(super::super::SERVER_KEEPALIVE_IDLE_TIMEOUT_SECS),
                Request::read_from(&mut reader),
            )
            .await
            {
                Ok(Ok(request)) => request,
                Ok(Err(err)) if err.kind() == ErrorKind::UnexpectedEof => break,
                Ok(Err(err)) => {
                    drop(response_tx);
                    let _ = writer_task.await;
                    return Err(err);
                }
                Err(_) => break,
            };
            let checksums = match request.message_type {
                MessageType::RegisterChunks | MessageType::UnregisterChunks => {
                    let count = request.ensure_control_batch()?;
                    Some(read_checksum_batch(&mut reader, count).await?)
                }
                _ => None,
            };
            let inbound = InboundRequest { request, checksums };
            let chunk_db = Arc::clone(&self.chunk_db);
            let peer_client = self.peer_client.clone();
            let metrics = Arc::clone(&self.metrics);
            let transport = self.transport;
            let response_tx = response_tx.clone();
            tokio::spawn(async move {
                let ctx = RequestContext {
                    chunk_db: &chunk_db,
                    peer_client: peer_client.as_deref(),
                    metrics: &metrics,
                    transport,
                };
                let response = Self::process_request(&ctx, inbound).await;
                let _ = response_tx.send(response);
            });
        }

        drop(response_tx);
        writer_task
            .await
            .map_err(|_| io::Error::other("writer task panicked"))??;
        Ok(())
    }

    async fn process_request(ctx: &RequestContext<'_>, inbound: InboundRequest) -> ResponseAction {
        let request = &inbound.request;
        match request.message_type {
            MessageType::GetChunk => GetRequest { ctx, request }.action(),
            MessageType::PrefetchChunk => {
                ResponseAction::Immediate(PrefetchRequest { ctx, request }.process().await)
            }
            MessageType::HealthCheck => {
                ResponseAction::Immediate(HealthCheckRequest { ctx, request }.process().await)
            }
            MessageType::StatsLocality => {
                ResponseAction::Immediate(StatsLocalityRequest { ctx, request }.process().await)
            }
            MessageType::RegisterChunk | MessageType::UnregisterChunk => {
                ResponseAction::Immediate(ControlRequest { ctx, request }.process().await)
            }
            MessageType::RegisterChunks | MessageType::UnregisterChunks => {
                ResponseAction::Immediate(
                    ControlBatchRequest {
                        ctx,
                        request,
                        checksums: inbound
                            .checksums
                            .expect("checksums must be present for batch control messages"),
                    }
                    .process()
                    .await,
                )
            }
        }
    }
}
