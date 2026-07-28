use super::*;
use crate::backend::chunkdb::CheckSumMethod;
use std::thread;

#[test]
fn test_circuit_breaker_rejects_after_failure() {
    let breaker = CircuitBreaker::new(Duration::from_secs(30));
    assert!(!breaker.should_reject());

    breaker.record_failure();
    assert!(breaker.should_reject());
}

#[test]
fn test_circuit_breaker_resets_on_success() {
    let breaker = CircuitBreaker::new(Duration::from_secs(30));
    breaker.record_failure();
    assert!(breaker.should_reject());

    breaker.record_success();
    assert!(!breaker.should_reject());
}

#[test]
fn test_circuit_breaker_allows_probe_after_cooldown() {
    let breaker = CircuitBreaker::new(Duration::from_millis(50));
    breaker.record_failure();
    assert!(breaker.should_reject());

    thread::sleep(Duration::from_millis(60));
    assert!(!breaker.should_reject());
}

#[test]
fn test_local_client_circuit_breaker_skips_connect_when_unavailable() {
    let runtime = PeerRuntime::new().unwrap();
    // Use a non-existent socket path so connection always fails.
    let sock = PathBuf::from("/tmp/imagefsd_test_nonexistent.sock");
    let client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(100));
    let checksum = CheckSum::from_data(b"circuit-breaker-test", CheckSumMethod::Blake3);

    // First call should actually attempt connection and fail.
    let start = Instant::now();
    assert!(!client.prefetch_chunk_blocking(&checksum));
    let first_elapsed = start.elapsed();

    // Second call should be short-circuited by the breaker.
    let start = Instant::now();
    assert!(!client.prefetch_chunk_blocking(&checksum));
    let second_elapsed = start.elapsed();

    // The short-circuited call should be at least 5x faster than the real attempt.
    assert!(
        second_elapsed < first_elapsed / 5,
        "circuit breaker did not skip: first={first_elapsed:?}, second={second_elapsed:?}"
    );
}

#[test]
fn test_health_checker_opens_breaker_when_server_unavailable() {
    let runtime = PeerRuntime::new().unwrap();
    let sock = PathBuf::from("/tmp/imagefsd_test_health_nonexistent.sock");
    let client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(100));

    // Manually close the breaker to simulate a previously-healthy state.
    client.breaker.record_success();
    assert!(!client.breaker.should_reject());

    client.start_health_checker();

    // Wait for the first health check tick to fire and detect the unavailable socket.
    wait_until(|| client.breaker.should_reject());
}

#[test]
fn test_health_checker_closes_breaker_when_server_available() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let sock = dir.path().join("health_check.sock");
    let server = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db),
        "127.0.0.1:0".parse().unwrap(),
        &sock,
        None,
    )
    .unwrap();
    let shutdown = server.shutdown_handle();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(300));

    // Breaker starts closed (default), manually open it to simulate prior failure.
    client.breaker.record_failure();
    assert!(client.breaker.should_reject());

    client.start_health_checker();

    // Wait for the health check to detect the running server and close the breaker.
    wait_until(|| !client.breaker.should_reject());

    shutdown.shutdown();
    handle.join().unwrap();
}
