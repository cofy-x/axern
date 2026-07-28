use crate::backend::chunkdb::{CheckSum, CheckSumOnDisk};
use crate::utils::now_epoch_secs;
use std::collections::{HashMap, VecDeque};
use std::net::SocketAddr;
use std::sync::RwLock;

#[derive(Debug, Default)]
pub struct PeerHitHints {
    recent_hits: RwLock<HashMap<SocketAddr, HitRecord>>,
}

#[derive(Debug, Clone, Default)]
struct HitRecord {
    hit_count: u64,
    last_hit_time: u64,
    recent_checksums: VecDeque<CheckSumOnDisk>,
}

impl PeerHitHints {
    pub fn record_hit(&self, addr: SocketAddr, checksum: CheckSum) {
        let now = now_epoch_secs();
        let mut hits = self.recent_hits.write().unwrap();
        let record = hits.entry(addr).or_default();
        record.hit_count += 1;
        record.last_hit_time = now;
        record.recent_checksums.push_back(checksum.into());
        while record.recent_checksums.len() > super::super::HINT_MAX_RECENT {
            record.recent_checksums.pop_front();
        }
    }

    #[allow(dead_code)]
    #[cfg(test)]
    pub fn score_peer(&self, addr: &SocketAddr) -> f64 {
        let hits = self.recent_hits.read().unwrap();
        let now = now_epoch_secs();
        hits.get(addr).map_or(0.0, |record| {
            let count_score = (record.hit_count as f64).ln_1p();
            let age = now.saturating_sub(record.last_hit_time) as f64;
            let decay = (-age / 60.0).exp();
            count_score * decay
        })
    }

    #[cfg(all(test, target_os = "linux"))]
    pub(in crate::backend::peer) fn expire_peer_for_test(&self, addr: &SocketAddr) {
        let mut hits = self.recent_hits.write().unwrap();
        if let Some(record) = hits.get_mut(addr) {
            record.last_hit_time = now_epoch_secs().saturating_sub(super::super::HINT_TTL_SECS + 1);
        }
    }

    pub(in crate::backend::peer) fn score_snapshot(&self) -> HashMap<SocketAddr, f64> {
        let hits = self.recent_hits.read().unwrap();
        let now = now_epoch_secs();
        hits.iter()
            .map(|(addr, record)| {
                let count_score = (record.hit_count as f64).ln_1p();
                let age = now.saturating_sub(record.last_hit_time) as f64;
                let decay = (-age / 60.0).exp();
                (*addr, count_score * decay)
            })
            .collect()
    }

    pub(in crate::backend::peer) fn gc_expired(&self) {
        let cutoff = now_epoch_secs().saturating_sub(super::super::HINT_TTL_SECS);
        let mut hits = self.recent_hits.write().unwrap();
        hits.retain(|_, record| record.last_hit_time >= cutoff);
    }

    pub fn hinted_peer_count(&self) -> u64 {
        self.gc_expired();
        self.recent_hits.read().unwrap().len() as u64
    }
}
