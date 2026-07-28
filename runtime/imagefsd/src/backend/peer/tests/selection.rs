use super::*;

#[test]
fn test_peer_health_miss_does_not_mark_unhealthy() {
    let tracker = PeerHealthTracker::default();
    let addr: SocketAddr = "127.0.0.1:9876".parse().unwrap();
    tracker.record_miss(addr);
    tracker.record_miss(addr);
    tracker.record_miss(addr);
    assert!(!tracker.is_unhealthy(&addr));
}

#[test]
fn test_peer_hit_hints_gc() {
    let hints = PeerHitHints::default();
    let addr: SocketAddr = "127.0.0.1:9876".parse().unwrap();
    let checksum = CheckSum::from_data(b"gc", CheckSumMethod::Blake3);
    hints.record_hit(addr, checksum);
    assert!(hints.score_peer(&addr) > 0.0);
    hints.expire_peer_for_test(&addr);
    hints.gc_expired();
    assert_eq!(hints.score_peer(&addr), 0.0);
}

#[test]
fn test_ranked_peers_prefers_healthy_then_score_then_rtt() {
    let runtime = PeerRuntime::new().unwrap();
    let a: SocketAddr = "127.0.0.1:3001".parse().unwrap();
    let b: SocketAddr = "127.0.0.1:3002".parse().unwrap();
    let c: SocketAddr = "127.0.0.1:3003".parse().unwrap();
    let client = PeerClient::new(runtime, Arc::new(StaticPeers::new(vec![a, b, c])));

    client.health.record_success(a, 80.0);
    client.health.record_success(b, 20.0);
    client.health.record_success(c, 10.0);
    client.health.record_failure(c);
    client.health.record_failure(c);
    client.health.record_failure(c);
    assert!(!client.health.is_unhealthy(&a));
    assert!(client.health.is_unhealthy(&c));
    assert!(client.health.get_rtt_ms(&b) < client.health.get_rtt_ms(&a));

    let scores = HashMap::from([(a, 0.0), (b, 2.0), (c, 100.0)]);
    let ranked = client.ranked_peers_with_scores(vec![c, a, b, a], &scores);

    assert_eq!(ranked, vec![b, a, c]);
}

#[test]
fn test_candidate_peers_filter_self_addr() {
    let runtime = PeerRuntime::new().unwrap();
    let self_addr: SocketAddr = "127.0.0.1:18080".parse().unwrap();
    let owner_addr: SocketAddr = "127.0.0.1:18081".parse().unwrap();
    let discovery = Arc::new(StaticPeers::new(vec![self_addr, owner_addr]));
    let chunk_index = Arc::new(TestChunkIndex {
        owners: vec![self_addr, owner_addr],
        ..Default::default()
    });
    let checksum = CheckSum::from_data(b"self-filter", CheckSumMethod::Blake3);
    let client = PeerClient::new(runtime.clone(), discovery)
        .with_local_addr(self_addr)
        .with_chunk_index(chunk_index);

    let (candidates, source) = runtime.block_on(client.candidate_peers(&checksum));
    assert_eq!(source, "index");
    assert_eq!(candidates, vec![owner_addr]);
}
