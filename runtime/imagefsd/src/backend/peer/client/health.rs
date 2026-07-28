use crate::utils::now_epoch_secs;
use opentelemetry::metrics::{CallbackRegistration, Meter};
use opentelemetry::KeyValue;
use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;
use std::sync::RwLock;
use tracing::debug;

#[derive(Debug, Default)]
pub struct PeerHealthTracker {
    peers: RwLock<HashMap<SocketAddr, PeerHealth>>,
}

#[derive(Debug, Clone)]
struct PeerHealth {
    avg_rtt_ms: f64,
    consecutive_failures: u32,
    last_failure_time: u64,
    total_requests: u64,
    total_hits: u64,
}

#[derive(Debug, Clone)]
pub(in crate::backend::peer) struct PeerHealthSnapshotEntry {
    pub(in crate::backend::peer) addr: SocketAddr,
    pub(in crate::backend::peer) avg_rtt_ms: f64,
    pub(in crate::backend::peer) healthy: bool,
}

#[derive(Debug, Clone, Default)]
pub(in crate::backend::peer) struct PeerHealthSnapshot {
    pub(in crate::backend::peer) entries: Vec<PeerHealthSnapshotEntry>,
    healthy_count: u64,
    unhealthy_count: u64,
}

impl Default for PeerHealth {
    fn default() -> Self {
        Self {
            avg_rtt_ms: 50.0,
            consecutive_failures: 0,
            last_failure_time: 0,
            total_requests: 0,
            total_hits: 0,
        }
    }
}

impl PeerHealthTracker {
    pub fn record_success(&self, addr: SocketAddr, rtt_ms: f64) {
        let mut peers = self.peers.write().unwrap();
        let entry = peers.entry(addr).or_default();
        entry.avg_rtt_ms = 0.3 * rtt_ms + 0.7 * entry.avg_rtt_ms;
        entry.consecutive_failures = 0;
        entry.total_requests += 1;
        entry.total_hits += 1;
    }

    pub fn record_failure(&self, addr: SocketAddr) {
        let mut peers = self.peers.write().unwrap();
        let entry = peers.entry(addr).or_default();
        entry.consecutive_failures += 1;
        entry.last_failure_time = now_epoch_secs();
        entry.total_requests += 1;
    }

    pub fn counts(&self) -> (u64, u64) {
        let snapshot = self.snapshot();
        (snapshot.healthy_count, snapshot.unhealthy_count)
    }

    pub fn record_miss(&self, addr: SocketAddr) {
        let mut peers = self.peers.write().unwrap();
        let entry = peers.entry(addr).or_default();
        entry.total_requests += 1;
    }

    #[allow(dead_code)]
    pub fn is_unhealthy(&self, addr: &SocketAddr) -> bool {
        let peers = self.peers.read().unwrap();
        match peers.get(addr) {
            Some(peer) => {
                peer.consecutive_failures >= 3
                    && now_epoch_secs().saturating_sub(peer.last_failure_time) < 30
            }
            None => false,
        }
    }

    #[allow(dead_code)]
    pub fn get_rtt_ms(&self, addr: &SocketAddr) -> f64 {
        let peers = self.peers.read().unwrap();
        peers.get(addr).map_or(50.0, |peer| peer.avg_rtt_ms)
    }

    pub(in crate::backend::peer) fn snapshot(&self) -> PeerHealthSnapshot {
        let peers = self.peers.read().unwrap();
        let mut snapshot = PeerHealthSnapshot::default();
        let now = now_epoch_secs();
        for (addr, peer) in peers.iter() {
            let healthy = !(peer.consecutive_failures >= 3
                && now.saturating_sub(peer.last_failure_time) < 30);
            if healthy {
                snapshot.healthy_count += 1;
            } else {
                snapshot.unhealthy_count += 1;
            }
            snapshot.entries.push(PeerHealthSnapshotEntry {
                addr: *addr,
                avg_rtt_ms: peer.avg_rtt_ms,
                healthy,
            });
        }
        snapshot
    }

    pub(in crate::backend::peer) fn register_metrics(
        self: &Arc<Self>,
        meter: &Meter,
    ) -> Vec<Box<dyn CallbackRegistration>> {
        let peer_rtt = meter
            .f64_observable_gauge("imagefsd.health.peer_rtt_ms")
            .with_description("Current EMA RTT per peer")
            .with_unit("ms")
            .init();
        let peer_status = meter
            .u64_observable_gauge("imagefsd.health.peer_status")
            .with_description("Peer status: 1 healthy, 0 unhealthy")
            .init();
        let peers_total = meter
            .u64_observable_gauge("imagefsd.health.peers_total")
            .with_description("Current total peers by health status")
            .init();

        let health_rtt = self.clone();
        let reg1 = meter
            .register_callback(
                &[
                    peer_rtt.as_any(),
                    peer_status.as_any(),
                    peers_total.as_any(),
                ],
                move |observer| {
                    let snapshot = health_rtt.snapshot();
                    for entry in &snapshot.entries {
                        let peer_attr = [KeyValue::new("peer", entry.addr.to_string())];
                        observer.observe_f64(&peer_rtt, entry.avg_rtt_ms, &peer_attr);
                        observer.observe_u64(
                            &peer_status,
                            if entry.healthy { 1 } else { 0 },
                            &peer_attr,
                        );
                    }
                    observer.observe_u64(
                        &peers_total,
                        snapshot.healthy_count,
                        &[KeyValue::new("status", "healthy")],
                    );
                    observer.observe_u64(
                        &peers_total,
                        snapshot.unhealthy_count,
                        &[KeyValue::new("status", "unhealthy")],
                    );
                },
            )
            .map_err(|err| debug!(err = debug(err), "failed to register peer health metrics"))
            .ok();

        reg1.into_iter().collect()
    }
}
