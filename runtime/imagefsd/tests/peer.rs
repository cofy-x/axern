#[path = "support/mod.rs"]
mod support;

#[cfg(target_os = "linux")]
#[path = "peer/system.rs"]
mod system;

#[cfg(all(target_os = "linux", feature = "redis-integration-tests"))]
#[path = "peer/redis.rs"]
mod redis;
