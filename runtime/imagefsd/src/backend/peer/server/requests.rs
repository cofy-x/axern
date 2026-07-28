use super::handler::{RequestContext, ResponseAction};
use crate::backend::chunkdb::CheckSum;
use crate::backend::peer::protocol::{MessageType, Request, WireResponse};
use crate::utils::now_epoch_secs;
use serde_json::{json, Value};
use tokio::task::block_in_place;
use tracing::debug;

pub(super) struct GetRequest<'a> {
    pub(super) ctx: &'a RequestContext<'a>,
    pub(super) request: &'a Request,
}

impl GetRequest<'_> {
    pub(super) fn action(&self) -> ResponseAction {
        let begin = std::time::Instant::now();
        match self.request.ensure_full_chunk() {
            Ok(()) => ResponseAction::GetChunk {
                request_id: self.request.request_id,
                checksum: self.request.checksum,
            },
            Err(err) => {
                debug!(err = debug(err), "invalid get request");
                self.ctx.metrics.record_request(
                    "get_chunk",
                    "error",
                    self.ctx.transport,
                    begin.elapsed().as_secs_f64() * 1000.0,
                    0,
                );
                ResponseAction::Immediate(WireResponse {
                    request_id: self.request.request_id,
                    status: super::super::STATUS_ERROR,
                    payload: Vec::new(),
                })
            }
        }
    }
}

pub(super) struct PrefetchRequest<'a> {
    pub(super) ctx: &'a RequestContext<'a>,
    pub(super) request: &'a Request,
}

impl PrefetchRequest<'_> {
    pub(super) async fn process(&self) -> WireResponse {
        let begin = std::time::Instant::now();
        let (status_code, status) = match self.request.ensure_full_chunk() {
            Ok(()) => match Self::prefetch_from_peer(self.ctx, self.request).await {
                Ok(true) => (super::super::STATUS_HIT, "hit"),
                Ok(false) => (super::super::STATUS_MISS, "miss"),
                Err(err) => {
                    debug!(err = debug(err), "prefetch failed");
                    (super::super::STATUS_ERROR, "error")
                }
            },
            Err(err) => {
                debug!(err = debug(err), "invalid prefetch request");
                (super::super::STATUS_ERROR, "error")
            }
        };
        self.ctx.metrics.record_request(
            "prefetch",
            status,
            self.ctx.transport,
            begin.elapsed().as_secs_f64() * 1000.0,
            0,
        );
        WireResponse {
            request_id: self.request.request_id,
            status: status_code,
            payload: Vec::new(),
        }
    }

    async fn prefetch_from_peer(
        ctx: &RequestContext<'_>,
        request: &Request,
    ) -> anyhow::Result<bool> {
        if block_in_place(|| ctx.chunk_db.has_chunk(&request.checksum))? {
            return Ok(true);
        }
        let Some(peer_client) = ctx.peer_client else {
            return Ok(false);
        };
        let Some(data) = peer_client.fetch_chunk(&request.checksum).await else {
            return Ok(false);
        };
        let data_cs = CheckSum::from_data(&data, request.checksum.method);
        if data_cs != request.checksum {
            return Ok(false);
        }
        block_in_place(|| ctx.chunk_db.add_chunk(&request.checksum, data))?;
        Ok(true)
    }
}

pub(super) struct HealthCheckRequest<'a> {
    pub(super) ctx: &'a RequestContext<'a>,
    pub(super) request: &'a Request,
}

impl HealthCheckRequest<'_> {
    pub(super) async fn process(&self) -> WireResponse {
        let begin = std::time::Instant::now();
        let status_code = match self.request.ensure_full_chunk() {
            Ok(()) => super::super::STATUS_HIT,
            Err(err) => {
                debug!(
                    err = debug(err),
                    transport = self.ctx.transport,
                    "invalid health check request"
                );
                super::super::STATUS_ERROR
            }
        };
        let status = if status_code == super::super::STATUS_HIT {
            "hit"
        } else {
            "error"
        };
        self.ctx.metrics.record_request(
            "health_check",
            status,
            self.ctx.transport,
            begin.elapsed().as_secs_f64() * 1000.0,
            0,
        );
        WireResponse {
            request_id: self.request.request_id,
            status: status_code,
            payload: Vec::new(),
        }
    }
}

pub(super) struct StatsLocalityRequest<'a> {
    pub(super) ctx: &'a RequestContext<'a>,
    pub(super) request: &'a Request,
}

impl StatsLocalityRequest<'_> {
    pub(super) async fn process(&self) -> WireResponse {
        let begin = std::time::Instant::now();
        let (status_code, status, payload) = if self.ctx.transport != "unix" {
            (super::super::STATUS_ERROR, "error", Vec::new())
        } else {
            match self.request.ensure_full_chunk() {
                Ok(()) => match self.build_payload() {
                    Ok(payload) => (super::super::STATUS_HIT, "hit", payload),
                    Err(err) => {
                        debug!(
                            err = debug(err),
                            transport = self.ctx.transport,
                            "build locality stats payload failed"
                        );
                        (super::super::STATUS_ERROR, "error", Vec::new())
                    }
                },
                Err(err) => {
                    debug!(
                        err = debug(err),
                        transport = self.ctx.transport,
                        "invalid locality stats request"
                    );
                    (super::super::STATUS_ERROR, "error", Vec::new())
                }
            }
        };
        self.ctx.metrics.record_request(
            "stats_locality",
            status,
            self.ctx.transport,
            begin.elapsed().as_secs_f64() * 1000.0,
            payload.len(),
        );
        WireResponse {
            request_id: self.request.request_id,
            status: status_code,
            payload,
        }
    }

    fn build_payload(&self) -> anyhow::Result<Vec<u8>> {
        let stats = block_in_place(|| self.ctx.chunk_db.get_stats())?;
        let (peer_healthy_count, peer_unhealthy_count, peer_hinted_count) = self
            .ctx
            .peer_client
            .map_or((0, 0, 0), |client| client.locality_counts());
        let payload = json!({
            "chunkdb_total_chunks": chunkdb_total_chunks(&stats),
            "chunkdb_used_bytes": chunkdb_used_bytes(&stats),
            "chunkdb_recent_access_age_secs": chunkdb_recent_access_age_secs(&stats),
            "peer_healthy_count": peer_healthy_count,
            "peer_unhealthy_count": peer_unhealthy_count,
            "peer_hinted_count": peer_hinted_count,
        });
        Ok(serde_json::to_vec(&payload)?)
    }
}

pub(super) struct ControlRequest<'a> {
    pub(super) ctx: &'a RequestContext<'a>,
    pub(super) request: &'a Request,
}

impl ControlRequest<'_> {
    pub(super) async fn process(&self) -> WireResponse {
        let begin = std::time::Instant::now();
        let request_type = if self.request.message_type == MessageType::RegisterChunk {
            "register"
        } else {
            "unregister"
        };
        let (status_code, status) = if self.ctx.transport != "unix" {
            (super::super::STATUS_ERROR, "error")
        } else {
            match self.request.ensure_full_chunk() {
                Ok(()) => match self.handle_index_control().await {
                    Ok(()) => (super::super::STATUS_HIT, "hit"),
                    Err(err) => {
                        debug!(
                            err = debug(err),
                            request_type = request_type,
                            "chunk index control request failed"
                        );
                        (super::super::STATUS_ERROR, "error")
                    }
                },
                Err(err) => {
                    debug!(
                        err = debug(err),
                        request_type = request_type,
                        "invalid chunk index control request"
                    );
                    (super::super::STATUS_ERROR, "error")
                }
            }
        };
        self.ctx.metrics.record_request(
            request_type,
            status,
            self.ctx.transport,
            begin.elapsed().as_secs_f64() * 1000.0,
            0,
        );
        WireResponse {
            request_id: self.request.request_id,
            status: status_code,
            payload: Vec::new(),
        }
    }

    async fn handle_index_control(&self) -> anyhow::Result<()> {
        let Some(index) = self
            .ctx
            .peer_client
            .and_then(|client| client.chunk_index.as_ref())
        else {
            return Ok(());
        };
        match self.request.message_type {
            MessageType::RegisterChunk => index.register(&self.request.checksum).await,
            MessageType::UnregisterChunk => index.unregister(&self.request.checksum).await,
            _ => Ok(()),
        }
    }
}

pub(super) struct ControlBatchRequest<'a> {
    pub(super) ctx: &'a RequestContext<'a>,
    pub(super) request: &'a Request,
    pub(super) checksums: Vec<CheckSum>,
}

impl ControlBatchRequest<'_> {
    pub(super) async fn process(&self) -> WireResponse {
        let begin = std::time::Instant::now();
        let request_type = if self.request.message_type == MessageType::RegisterChunks {
            "register_batch"
        } else {
            "unregister_batch"
        };
        let (status_code, status) = if self.ctx.transport != "unix" {
            (super::super::STATUS_ERROR, "error")
        } else {
            match self.request.ensure_control_batch() {
                Ok(count) if count == self.checksums.len() => {
                    match self.handle_control_batch().await {
                        Ok(()) => (super::super::STATUS_HIT, "hit"),
                        Err(err) => {
                            debug!(
                                err = debug(err),
                                count = self.checksums.len(),
                                "batch chunk index control request failed"
                            );
                            (super::super::STATUS_ERROR, "error")
                        }
                    }
                }
                Ok(count) => {
                    debug!(
                        count = count,
                        actual = self.checksums.len(),
                        "invalid batch control request length"
                    );
                    (super::super::STATUS_ERROR, "error")
                }
                Err(err) => {
                    debug!(err = debug(err), "invalid batch control request header");
                    (super::super::STATUS_ERROR, "error")
                }
            }
        };
        self.ctx.metrics.record_request(
            request_type,
            status,
            self.ctx.transport,
            begin.elapsed().as_secs_f64() * 1000.0,
            0,
        );
        WireResponse {
            request_id: self.request.request_id,
            status: status_code,
            payload: Vec::new(),
        }
    }

    async fn handle_control_batch(&self) -> anyhow::Result<()> {
        let Some(index) = self
            .ctx
            .peer_client
            .and_then(|client| client.chunk_index.as_ref())
        else {
            return Ok(());
        };
        match self.request.message_type {
            MessageType::RegisterChunks => index.register_batch(&self.checksums).await,
            MessageType::UnregisterChunks => index.unregister_batch(&self.checksums).await,
            _ => Ok(()),
        }
    }
}

fn chunkdb_total_chunks(stats: &Value) -> u64 {
    stats
        .get("chunks")
        .and_then(|value| value.get("total_count"))
        .and_then(Value::as_u64)
        .unwrap_or(0)
}

fn chunkdb_used_bytes(stats: &Value) -> u64 {
    stats
        .get("storage")
        .and_then(|value| value.get("used_size_bytes"))
        .and_then(Value::as_u64)
        .unwrap_or(0)
}

fn chunkdb_recent_access_age_secs(stats: &Value) -> u64 {
    let Some(newest_epoch_secs) = stats
        .get("access_time")
        .and_then(|value| value.get("newest_epoch_secs"))
        .and_then(Value::as_u64)
    else {
        return 0;
    };
    now_epoch_secs().saturating_sub(newest_epoch_secs)
}
