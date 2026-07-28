use std::sync::{Arc, Condvar, Mutex};
use std::time::Duration;

const IO_WAIT_TIMEOUT: u64 = 120;

#[derive(Debug, Copy, Clone, Eq, PartialEq)]
enum InFlightStatus {
    Fetching,
    Done,
}

#[derive(Debug)]
pub(crate) struct InFlightIO {
    state: Mutex<InFlightStatus>,
    cond: Condvar,
}

impl InFlightIO {
    pub(crate) fn new() -> Self {
        Self {
            state: Mutex::new(InFlightStatus::Fetching),
            cond: Condvar::new(),
        }
    }

    fn notify(&self) {
        self.cond.notify_all();
    }

    pub(crate) fn done(&self) {
        let mut state = self.state.lock().unwrap();
        *state = InFlightStatus::Done;
        drop(state);
        self.notify();
    }

    pub(crate) fn wait(&self) -> bool {
        let state = self.state.lock().unwrap();
        if *state == InFlightStatus::Fetching {
            let r = self
                .cond
                .wait_timeout(state, Duration::from_secs(IO_WAIT_TIMEOUT))
                .unwrap();
            r.1.timed_out()
        } else {
            false
        }
    }
}

pub(crate) struct ProcessIOGuard(pub(crate) Arc<InFlightIO>);

impl Drop for ProcessIOGuard {
    fn drop(&mut self) {
        self.0.done();
    }
}
