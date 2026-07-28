use super::*;
use crate::test_metrics::{
    gauge_points_f64, gauge_points_u64, histogram_points_f64, sum_points_u64, MetricsHarness,
};
use std::collections::BTreeMap;
use std::net::TcpListener as StdTcpListener;

fn attrs(pairs: &[(&str, &str)]) -> BTreeMap<String, String> {
    pairs
        .iter()
        .map(|(key, value)| ((*key).to_string(), (*value).to_string()))
        .collect()
}

#[test]
fn test_peer_health_observable_gauges_emit_expected_labels() {
    let harness = MetricsHarness::new();
    let health = Arc::new(PeerHealthTracker::default());
    let _metrics = health.register_metrics(&harness.meter("imagefsd.health.test"));
    let peer: SocketAddr = "127.0.0.1:4317".parse().unwrap();

    health.record_success(peer, 10.0);

    let collected = harness.collect();
    assert_eq!(
        gauge_points_f64(&collected, "imagefsd.health.peer_rtt_ms"),
        vec![(attrs(&[("peer", "127.0.0.1:4317")]), 38.0)]
    );
    assert_eq!(
        gauge_points_u64(&collected, "imagefsd.health.peer_status"),
        vec![(attrs(&[("peer", "127.0.0.1:4317")]), 1)]
    );
    assert!(gauge_points_u64(&collected, "imagefsd.health.peers_total")
        .contains(&(attrs(&[("status", "healthy")]), 1)));
    assert!(gauge_points_u64(&collected, "imagefsd.health.peers_total")
        .contains(&(attrs(&[("status", "unhealthy")]), 0)));
}

#[test]
fn test_peer_fetch_hit_emits_metrics() {
    let runtime = PeerRuntime::new().unwrap();
    let harness = MetricsHarness::new();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let data = b"peer-metrics-hit".to_vec();
    let checksum = CheckSum::from_data(&data, CheckSumMethod::Blake3);
    db.add_chunk(&checksum, data.clone()).unwrap();

    let sock = dir.path().join("chunkserver.sock");
    let server = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db),
        "127.0.0.1:0".parse().unwrap(),
        &sock,
        None,
    )
    .unwrap();
    let shutdown = server.shutdown_handle();
    let addr = server.tcp_listener.local_addr().unwrap();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let discovery = Arc::new(StaticPeers::new(vec![addr]));
    let mut client =
        PeerClient::new(runtime.clone(), discovery).with_timeout(Duration::from_millis(300));
    client.metrics = PeerClientMetrics::new(&harness.meter("imagefsd.peer.test"));

    assert_eq!(client.fetch_chunk_blocking(&checksum), Some(data));

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.peer.fetch_total")
        .contains(&(attrs(&[("source", "random"), ("result", "hit")]), 1)));
    assert!(
        histogram_points_f64(&collected, "imagefsd.peer.fetch_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs == &attrs(&[("source", "random"), ("result", "hit")]) && *count == 1
            })
    );
    assert!(sum_points_u64(&collected, "imagefsd.peer.query_total")
        .contains(&(attrs(&[("result", "hit")]), 1)));

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_peer_fetch_miss_emits_metrics() {
    let runtime = PeerRuntime::new().unwrap();
    let harness = MetricsHarness::new();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let checksum = CheckSum::from_data(b"peer-metrics-miss", CheckSumMethod::Blake3);

    let sock = dir.path().join("chunkserver.sock");
    let server = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db),
        "127.0.0.1:0".parse().unwrap(),
        &sock,
        None,
    )
    .unwrap();
    let shutdown = server.shutdown_handle();
    let addr = server.tcp_listener.local_addr().unwrap();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let discovery = Arc::new(StaticPeers::new(vec![addr]));
    let mut client =
        PeerClient::new(runtime.clone(), discovery).with_timeout(Duration::from_millis(300));
    client.metrics = PeerClientMetrics::new(&harness.meter("imagefsd.peer.test"));

    assert_eq!(client.fetch_chunk_blocking(&checksum), None);

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.peer.fetch_total")
        .contains(&(attrs(&[("source", "random"), ("result", "miss")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.peer.query_total")
        .contains(&(attrs(&[("result", "miss")]), 1)));

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_peer_fetch_timeout_emits_metrics() {
    let runtime = PeerRuntime::new().unwrap();
    let harness = MetricsHarness::new();
    let checksum = CheckSum::from_data(b"peer-metrics-timeout", CheckSumMethod::Blake3);
    let listener = StdTcpListener::bind("127.0.0.1:0").unwrap();
    let addr = listener.local_addr().unwrap();
    let hang = thread::spawn(move || {
        let (_stream, _) = listener.accept().unwrap();
        thread::sleep(Duration::from_millis(300));
    });

    let discovery = Arc::new(StaticPeers::new(vec![addr]));
    let mut client =
        PeerClient::new(runtime.clone(), discovery).with_timeout(Duration::from_millis(100));
    client.metrics = PeerClientMetrics::new(&harness.meter("imagefsd.peer.test"));

    assert_eq!(client.fetch_chunk_blocking(&checksum), None);

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.peer.fetch_total")
        .contains(&(attrs(&[("source", "random"), ("result", "timeout")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.peer.query_total")
        .contains(&(attrs(&[("result", "timeout")]), 1)));

    hang.join().unwrap();
}

#[test]
fn test_peer_retry_emits_index_source_metrics() {
    let runtime = PeerRuntime::new().unwrap();
    let harness = MetricsHarness::new();
    let dir_a = TempDir::new().unwrap();
    let dir_b = TempDir::new().unwrap();
    let checksum = CheckSum::from_data(b"peer-metrics-retry", CheckSumMethod::Blake3);
    let data = b"peer-metrics-retry".to_vec();

    let db_a = Arc::new(ChunkDB::new(dir_a.path()).unwrap());
    let sock_a = dir_a.path().join("chunkserver.sock");
    let server_a = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db_a),
        "127.0.0.1:0".parse().unwrap(),
        &sock_a,
        None,
    )
    .unwrap();
    let shutdown_a = server_a.shutdown_handle();
    let addr_a = server_a.tcp_listener.local_addr().unwrap();
    let handle_a = thread::spawn(move || server_a.run().unwrap());
    wait_until(|| sock_a.exists());

    let db_b = Arc::new(ChunkDB::new(dir_b.path()).unwrap());
    db_b.add_chunk(&checksum, data.clone()).unwrap();
    let sock_b = dir_b.path().join("chunkserver.sock");
    let server_b = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db_b),
        "127.0.0.1:0".parse().unwrap(),
        &sock_b,
        None,
    )
    .unwrap();
    let shutdown_b = server_b.shutdown_handle();
    let addr_b = server_b.tcp_listener.local_addr().unwrap();
    let handle_b = thread::spawn(move || server_b.run().unwrap());
    wait_until(|| sock_b.exists());

    let chunk_index = Arc::new(TestChunkIndex {
        owners: vec![addr_a, addr_b],
        ..Default::default()
    });
    let mut client = PeerClient::new(runtime.clone(), Arc::new(StaticPeers::new(Vec::new())))
        .with_chunk_index(chunk_index)
        .with_timeout(Duration::from_millis(300));
    client.metrics = PeerClientMetrics::new(&harness.meter("imagefsd.peer.test"));

    assert_eq!(client.fetch_chunk_blocking(&checksum), Some(data));

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.peer.retry_total")
        .contains(&(attrs(&[("source", "index")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.peer.fetch_total")
        .contains(&(attrs(&[("source", "index"), ("result", "hit")]), 1)));

    shutdown_a.shutdown();
    shutdown_b.shutdown();
    handle_a.join().unwrap();
    handle_b.join().unwrap();
}

#[test]
fn test_chunkserver_transport_metrics_cover_tcp_and_unix() {
    let runtime = PeerRuntime::new().unwrap();
    let harness = MetricsHarness::new();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let sock = dir.path().join("chunkserver.sock");
    let mut server = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db),
        "127.0.0.1:0".parse().unwrap(),
        &sock,
        None,
    )
    .unwrap();
    server.metrics = Arc::new(ChunkServerMetrics::new(
        &harness.meter("imagefsd.chunkserver.test"),
    ));
    let shutdown = server.shutdown_handle();
    let addr = server.tcp_listener.local_addr().unwrap();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let mut tcp_stream = StdTcpStream::connect(addr).unwrap();
    Request::whole_chunk(MessageType::HealthCheck, CheckSum::empty())
        .write_to_sync(&mut tcp_stream)
        .unwrap();
    let _ = WireResponse::read_from_sync(&mut tcp_stream).unwrap();

    let mut unix_stream = StdUnixStream::connect(&sock).unwrap();
    Request::whole_chunk(MessageType::HealthCheck, CheckSum::empty())
        .write_to_sync(&mut unix_stream)
        .unwrap();
    let _ = WireResponse::read_from_sync(&mut unix_stream).unwrap();

    let collected = harness.collect();
    assert!(
        sum_points_u64(&collected, "imagefsd.server.requests_total").contains(&(
            attrs(&[
                ("type", "health_check"),
                ("status", "hit"),
                ("transport", "tcp")
            ]),
            1
        ))
    );
    assert!(
        sum_points_u64(&collected, "imagefsd.server.requests_total").contains(&(
            attrs(&[
                ("type", "health_check"),
                ("status", "hit"),
                ("transport", "unix")
            ]),
            1
        ))
    );
    assert!(
        histogram_points_f64(&collected, "imagefsd.server.request_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs
                    == &attrs(&[
                        ("type", "health_check"),
                        ("status", "hit"),
                        ("transport", "tcp"),
                    ])
                    && *count == 1
            })
    );
    assert!(
        histogram_points_f64(&collected, "imagefsd.server.request_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs
                    == &attrs(&[
                        ("type", "health_check"),
                        ("status", "hit"),
                        ("transport", "unix"),
                    ])
                    && *count == 1
            })
    );

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_local_control_metrics_emit_op_and_result() {
    let runtime = PeerRuntime::new().unwrap();
    let harness = MetricsHarness::new();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let sock = dir.path().join("chunkserver.sock");
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

    let checksum = CheckSum::from_data(b"local-metrics", CheckSumMethod::Blake3);
    let mut client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(300));
    client.metrics = LocalClientMetrics::new(&harness.meter("imagefsd.local.test"));

    assert!(!client.prefetch_chunk_blocking(&checksum));
    assert!(client.register_local_chunk(&checksum));

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.local.request_total")
        .contains(&(attrs(&[("op", "prefetch"), ("result", "miss")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.local.request_total")
        .contains(&(attrs(&[("op", "register"), ("result", "hit")]), 1)));
    assert!(
        histogram_points_f64(&collected, "imagefsd.local.request_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs == &attrs(&[("op", "prefetch"), ("result", "miss")]) && *count == 1
            })
    );

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_chunk_index_metrics_emit_new_totals() {
    let harness = MetricsHarness::new();
    let metrics = ChunkIndexMetrics::new(&harness.meter("imagefsd.index.test"));

    metrics.record_lookup("hit", 3.5, 2);
    metrics.record_register_attempt("batch", 3);
    metrics.record_register_success("batch", 2);
    metrics.record_unregister("batch", 1);
    metrics.record_refresh("ok", 4);
    metrics.record_repair("ok", 5);
    metrics.record_error("lookup");

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.index.lookup_total")
        .contains(&(attrs(&[("result", "hit")]), 1)));
    assert!(
        histogram_points_f64(&collected, "imagefsd.index.lookup_duration_ms").contains(&(
            attrs(&[("result", "hit")]),
            1,
            3.5
        ))
    );
    assert!(sum_points_u64(&collected, "imagefsd.index.register_total")
        .contains(&(attrs(&[("mode", "batch")]), 3)));
    assert!(
        sum_points_u64(&collected, "imagefsd.index.register_success_total")
            .contains(&(attrs(&[("mode", "batch")]), 2))
    );
    assert!(
        sum_points_u64(&collected, "imagefsd.index.unregister_total")
            .contains(&(attrs(&[("mode", "batch")]), 1))
    );
    assert!(sum_points_u64(&collected, "imagefsd.index.refresh_total")
        .contains(&(attrs(&[("result", "ok")]), 4)));
    assert!(sum_points_u64(&collected, "imagefsd.index.repair_total")
        .contains(&(attrs(&[("result", "ok")]), 5)));
    assert!(sum_points_u64(&collected, "imagefsd.index.error_total")
        .contains(&(attrs(&[("op", "lookup")]), 1)));
}
