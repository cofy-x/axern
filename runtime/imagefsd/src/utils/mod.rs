use lazy_static::lazy_static;
use num_traits::PrimInt;
use std::fmt::Display;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

lazy_static! {
    static ref PAGE_SIZE: u64 = {
        let v = unsafe { libc::sysconf(libc::_SC_PAGESIZE) };
        if v <= 0 {
            4096
        } else {
            v as u64
        }
    };
}

/// Aligns a value `v` up to the next multiple of `align`.
///
/// This function is generic over all primitive integer types.
///
/// # Panics
/// Panics if `align` is zero.
///
/// # Arguments
/// * `v`: The value to align.
/// * `align`: The alignment boundary. Must be non-zero.
///
/// # Returns
/// The aligned value.
pub fn align_up<T>(v: T, align: T) -> T
where
    T: PrimInt, // `PrimInt` trait provides common integer operations.
{
    // Ensure `align` is not zero to prevent division by zero.
    if align.is_zero() {
        panic!("align_up: alignment cannot be zero.");
    }

    // The classic alignment formula: (v + align - 1) / align * align
    // Let's break it down to be safe and clear.
    let one = T::one();
    let align_minus_one = align - one;
    let numerator = v
        .checked_add(&align_minus_one)
        .unwrap_or_else(|| panic!("align_up: overflow when aligning value."));
    numerator / align * align
}

pub fn new_std_io_error<T: Display>(t: T) -> std::io::Error {
    std::io::Error::other(t.to_string())
}

pub fn page_size() -> u64 {
    *PAGE_SIZE
}

/// Lock-free, thread-safe log rate limiter.
///
/// Suppresses repeated log calls within a configurable time interval.
/// Usable in `static` context via `const fn new()`.
///
/// # Example
/// ```ignore
/// static LIMITER: RateLimitedLog = RateLimitedLog::new(30);
/// if LIMITER.should_log() {
///     warn!("something slow happened");
/// }
/// ```
pub struct RateLimitedLog {
    last_epoch_secs: AtomicU64,
    interval_secs: u64,
}

impl RateLimitedLog {
    pub const fn new(interval_secs: u64) -> Self {
        Self {
            last_epoch_secs: AtomicU64::new(0),
            interval_secs,
        }
    }

    pub fn should_log(&self) -> bool {
        let now = now_epoch_secs();
        let prev = self.last_epoch_secs.load(Ordering::Relaxed);
        now >= prev + self.interval_secs
            && self
                .last_epoch_secs
                .compare_exchange(prev, now, Ordering::Relaxed, Ordering::Relaxed)
                .is_ok()
    }
}

/// Convenience macro for rate-limited logging.
///
/// # Example
/// ```ignore
/// static LIMITER: RateLimitedLog = RateLimitedLog::new(30);
/// rate_limited_log!(LIMITER, tracing::warn!("slow operation took {:?}", elapsed));
/// ```
#[macro_export]
macro_rules! rate_limited_log {
    ($limiter:expr, $($log:tt)+) => {
        if $limiter.should_log() {
            $($log)+
        }
    };
}

pub fn now_epoch_secs() -> u64 {
    match SystemTime::now().duration_since(UNIX_EPOCH) {
        Ok(d) => d.as_secs(),
        Err(_) => 0,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_align_up_unsigned() {
        assert_eq!(align_up(0u32, 8), 0);
        assert_eq!(align_up(1u32, 8), 8);
        assert_eq!(align_up(7u32, 8), 8);
        assert_eq!(align_up(8u32, 8), 8);
        assert_eq!(align_up(9u32, 8), 16);
        assert_eq!(align_up(16u64, 4096), 4096);
    }

    #[test]
    #[should_panic]
    fn test_align_up_zero_alignment() {
        align_up(10u32, 0);
    }
}
