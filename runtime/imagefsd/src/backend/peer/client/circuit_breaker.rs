use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::time::{Duration, Instant};

#[derive(Debug)]
pub(in crate::backend::peer) struct CircuitBreaker {
    open: AtomicBool,
    last_failure_ms: AtomicU64,
    cooldown: Duration,
    epoch: Instant,
}

impl CircuitBreaker {
    pub(in crate::backend::peer) fn new(cooldown: Duration) -> Self {
        Self {
            open: AtomicBool::new(false),
            last_failure_ms: AtomicU64::new(0),
            cooldown,
            epoch: Instant::now(),
        }
    }

    pub(in crate::backend::peer) fn should_reject(&self) -> bool {
        if !self.open.load(Ordering::Acquire) {
            return false;
        }
        let last = self.last_failure_ms.load(Ordering::Relaxed);
        let now = self.epoch.elapsed().as_millis() as u64;
        now.saturating_sub(last) < self.cooldown.as_millis() as u64
    }

    pub(in crate::backend::peer) fn record_success(&self) {
        self.open.store(false, Ordering::Release);
    }

    pub(in crate::backend::peer) fn record_failure(&self) {
        let now = self.epoch.elapsed().as_millis() as u64;
        self.last_failure_ms.store(now, Ordering::Relaxed);
        self.open.store(true, Ordering::Release);
    }

    #[cfg(all(test, target_os = "linux"))]
    pub(in crate::backend::peer) fn last_failure_ms(&self) -> u64 {
        self.last_failure_ms.load(Ordering::Relaxed)
    }
}

impl Clone for CircuitBreaker {
    fn clone(&self) -> Self {
        Self {
            open: AtomicBool::new(self.open.load(Ordering::Relaxed)),
            last_failure_ms: AtomicU64::new(self.last_failure_ms.load(Ordering::Relaxed)),
            cooldown: self.cooldown,
            epoch: self.epoch,
        }
    }
}
