#![cfg(all(target_os = "linux", feature = "redis-integration-tests"))]

use crate::support::{
    checksum_for, redis_remove_owner, redis_set_string, unique_node_id, wait_for_next_epoch_second,
    wait_until, wait_until_with_timeout, RedisTestGuard,
};
use imagefsd::backend::chunkdb::{CheckSum, CheckSumMethod, ChunkDB};
use imagefsd::backend::peer::{
    ChunkIndex, ChunkServer, LocalChunkClient, PeerClient, PeerDiscovery, PeerRuntime,
    RedisChunkIndex, RedisDiscovery,
};
use std::net::SocketAddr;
use std::sync::Arc;
use std::thread;
use std::time::Duration;
use tempfile::TempDir;

fn same_peers(actual: &[SocketAddr], expected: &[SocketAddr]) -> bool {
    let mut actual = actual.to_vec();
    let mut expected = expected.to_vec();
    actual.sort_unstable();
    expected.sort_unstable();
    actual == expected
}

#[test]
fn redis_chunk_index_register_lookup_unregister_roundtrip() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let checksum = checksum_for("redis-roundtrip");
    let advertise_addr: SocketAddr = "127.0.0.1:21001".parse().unwrap();
    let index = RedisChunkIndex::new(
        redis.url(),
        advertise_addr,
        &unique_node_id("redis-roundtrip"),
        30,
    )
    .unwrap();

    runtime.block_on(async {
        assert!(index.lookup_owners(&checksum).await.unwrap().is_empty());
        index.register(&checksum).await.unwrap();
        assert_eq!(
            index.lookup_owners(&checksum).await.unwrap(),
            vec![advertise_addr]
        );
        index.unregister(&checksum).await.unwrap();
        assert!(index.lookup_owners(&checksum).await.unwrap().is_empty());
    });
}

#[test]
fn redis_chunk_index_register_batch_caps_owner_count() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let checksum = checksum_for("redis-owner-cap");
    let owners = [
        "127.0.0.1:21101".parse().unwrap(),
        "127.0.0.1:21102".parse().unwrap(),
        "127.0.0.1:21103".parse().unwrap(),
        "127.0.0.1:21104".parse().unwrap(),
    ];

    for (idx, owner) in owners.iter().copied().enumerate() {
        if idx > 0 {
            wait_for_next_epoch_second();
        }
        let index =
            RedisChunkIndex::new(redis.url(), owner, &unique_node_id("redis-owner-cap"), 30)
                .unwrap();
        runtime.block_on(index.register_batch(&[checksum])).unwrap();
    }

    let lookup = RedisChunkIndex::new(
        redis.url(),
        owners[3],
        &unique_node_id("redis-owner-cap-lookup"),
        30,
    )
    .unwrap();
    let resolved = runtime.block_on(lookup.lookup_owners(&checksum)).unwrap();

    assert_eq!(resolved.len(), 3);
    assert!(!resolved.contains(&owners[0]));
    assert!(resolved.contains(&owners[1]));
    assert!(resolved.contains(&owners[2]));
    assert!(resolved.contains(&owners[3]));
}

#[test]
fn redis_chunk_index_refresh_registered_extends_ttl() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let checksum = checksum_for("redis-refresh-ttl");
    let advertise_addr: SocketAddr = "127.0.0.1:21201".parse().unwrap();
    let index = RedisChunkIndex::new(
        redis.url(),
        advertise_addr,
        &unique_node_id("redis-refresh"),
        3,
    )
    .unwrap();

    runtime.block_on(index.register(&checksum)).unwrap();
    thread::sleep(Duration::from_secs(1));
    assert_eq!(
        runtime
            .block_on(index.refresh_registered(Duration::from_secs(1)))
            .unwrap(),
        Some(1)
    );

    thread::sleep(Duration::from_millis(2500));
    assert_eq!(
        runtime.block_on(index.lookup_owners(&checksum)).unwrap(),
        vec![advertise_addr]
    );

    thread::sleep(Duration::from_millis(1500));
    assert!(runtime
        .block_on(index.lookup_owners(&checksum))
        .unwrap()
        .is_empty());
}

#[test]
fn redis_chunk_index_repair_missing_owners_restores_deleted_entry() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let checksum = checksum_for("redis-repair");
    let advertise_addr: SocketAddr = "127.0.0.1:21301".parse().unwrap();
    let index = RedisChunkIndex::new(
        redis.url(),
        advertise_addr,
        &unique_node_id("redis-repair"),
        30,
    )
    .unwrap();

    runtime.block_on(index.register(&checksum)).unwrap();
    redis_remove_owner(redis.url(), &checksum, advertise_addr);
    assert!(runtime
        .block_on(index.lookup_owners(&checksum))
        .unwrap()
        .is_empty());

    let repair_index = RedisChunkIndex::new(
        redis.url(),
        advertise_addr,
        &unique_node_id("redis-repair"),
        30,
    )
    .unwrap();
    let repaired = runtime
        .block_on(repair_index.repair_missing_owners(&[checksum]))
        .unwrap();
    assert_eq!(repaired, Some(1));
    assert_eq!(
        runtime
            .block_on(repair_index.lookup_owners(&checksum))
            .unwrap(),
        vec![advertise_addr]
    );
}

#[test]
fn redis_discovery_registers_and_removes_peers() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let advertise_a: SocketAddr = "127.0.0.1:21401".parse().unwrap();
    let advertise_b: SocketAddr = "127.0.0.1:21402".parse().unwrap();
    let observer_addr: SocketAddr = "127.0.0.1:21499".parse().unwrap();
    let discovery_a = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            advertise_a,
            &unique_node_id("redis-discovery-a"),
        )
        .unwrap(),
    );
    let discovery_b = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            advertise_b,
            &unique_node_id("redis-discovery-b"),
        )
        .unwrap(),
    );

    wait_until_with_timeout(Duration::from_secs(10), || {
        discovery_b.get_peers().contains(&advertise_a)
    });

    let observer = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            observer_addr,
            &unique_node_id("redis-discovery-observer"),
        )
        .unwrap(),
    );
    wait_until_with_timeout(Duration::from_secs(10), || {
        same_peers(&observer.get_peers(), &[advertise_a, advertise_b])
    });

    discovery_a.shutdown();

    let after_shutdown = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            "127.0.0.1:21500".parse().unwrap(),
            &unique_node_id("redis-discovery-after-shutdown"),
        )
        .unwrap(),
    );
    wait_until_with_timeout(Duration::from_secs(10), || {
        let peers = after_shutdown.get_peers();
        peers.contains(&advertise_b) && !peers.contains(&advertise_a)
    });

    after_shutdown.shutdown();
    observer.shutdown();
    discovery_b.shutdown();
}

#[test]
fn redis_discovery_ignores_invalid_peer_values() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let self_addr: SocketAddr = "127.0.0.1:21501".parse().unwrap();
    let valid_peer: SocketAddr = "127.0.0.1:21502".parse().unwrap();

    redis_set_string(
        redis.url(),
        "imagefsd:peers:invalid",
        "not-a-socket-addr",
        60,
    );
    redis_set_string(
        redis.url(),
        "imagefsd:peers:valid",
        &valid_peer.to_string(),
        60,
    );
    redis_set_string(
        redis.url(),
        "imagefsd:peers:self",
        &self_addr.to_string(),
        60,
    );

    let discovery = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            self_addr,
            &unique_node_id("redis-discovery-invalid"),
        )
        .unwrap(),
    );

    wait_until_with_timeout(Duration::from_secs(10), || {
        discovery.get_peers() == vec![valid_peer]
    });

    discovery.shutdown();
}

#[test]
fn redis_end_to_end_prefetch_via_discovery_and_chunk_index() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let server_a_dir = TempDir::new().unwrap();
    let server_b_dir = TempDir::new().unwrap();
    let db_a = Arc::new(ChunkDB::new(server_a_dir.path()).unwrap());
    let db_b = Arc::new(ChunkDB::new(server_b_dir.path()).unwrap());
    let data = b"redis-end-to-end".to_vec();
    let checksum = CheckSum::from_data(&data, CheckSumMethod::Blake3);
    db_a.add_chunk(&checksum, data.clone()).unwrap();

    let sock_a = server_a_dir.path().join("chunkserver.sock");
    let sock_b = server_b_dir.path().join("chunkserver.sock");
    let advertise_a: SocketAddr = "127.0.0.1:21601".parse().unwrap();
    let advertise_b: SocketAddr = "127.0.0.1:21602".parse().unwrap();
    let discovery_a = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            advertise_a,
            &unique_node_id("redis-e2e-a"),
        )
        .unwrap(),
    );
    let discovery_b = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            advertise_b,
            &unique_node_id("redis-e2e-b"),
        )
        .unwrap(),
    );
    let chunk_index_a = Arc::new(
        RedisChunkIndex::new(
            redis.url(),
            advertise_a,
            &unique_node_id("redis-e2e-index-a"),
            30,
        )
        .unwrap(),
    );
    let chunk_index_b = Arc::new(
        RedisChunkIndex::new(
            redis.url(),
            advertise_b,
            &unique_node_id("redis-e2e-index-b"),
            30,
        )
        .unwrap(),
    );

    let peer_client_a = Arc::new(
        PeerClient::new(runtime.clone(), discovery_a.clone())
            .with_local_addr(advertise_a)
            .with_chunk_index(chunk_index_a.clone()),
    );
    let peer_client_b = Arc::new(
        PeerClient::new(runtime.clone(), discovery_b.clone())
            .with_local_addr(advertise_b)
            .with_chunk_index(chunk_index_b.clone()),
    );

    let server_a = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db_a),
        advertise_a,
        &sock_a,
        Some(peer_client_a),
    )
    .unwrap();
    let server_b = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db_b),
        advertise_b,
        &sock_b,
        Some(peer_client_b),
    )
    .unwrap();

    let shutdown_a = server_a.shutdown_handle();
    let shutdown_b = server_b.shutdown_handle();
    let handle_a = thread::spawn(move || server_a.run().unwrap());
    let handle_b = thread::spawn(move || server_b.run().unwrap());

    wait_until(|| sock_a.exists());
    wait_until(|| sock_b.exists());
    wait_until_with_timeout(Duration::from_secs(10), || {
        discovery_b.get_peers().contains(&advertise_a)
    });
    wait_until(|| {
        runtime
            .block_on(chunk_index_b.lookup_owners(&checksum))
            .unwrap()
            .contains(&advertise_a)
    });

    let local_client_b = LocalChunkClient::new(runtime, &sock_b, Duration::from_millis(500));
    assert!(local_client_b.prefetch_chunk_blocking(&checksum));
    assert_eq!(db_b.get_chunk(&checksum).unwrap().unwrap(), data);

    shutdown_b.shutdown();
    shutdown_a.shutdown();
    discovery_b.shutdown();
    discovery_a.shutdown();
    handle_b.join().unwrap();
    handle_a.join().unwrap();
}

#[test]
fn redis_end_to_end_prefetch_via_discovery_without_chunk_index() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let server_a_dir = TempDir::new().unwrap();
    let server_b_dir = TempDir::new().unwrap();
    let db_a = Arc::new(ChunkDB::new(server_a_dir.path()).unwrap());
    let db_b = Arc::new(ChunkDB::new(server_b_dir.path()).unwrap());
    let data = b"redis-discovery-only".to_vec();
    let checksum = CheckSum::from_data(&data, CheckSumMethod::Blake3);
    db_a.add_chunk(&checksum, data.clone()).unwrap();

    let sock_a = server_a_dir.path().join("chunkserver.sock");
    let sock_b = server_b_dir.path().join("chunkserver.sock");
    let advertise_a: SocketAddr = "127.0.0.1:21701".parse().unwrap();
    let advertise_b: SocketAddr = "127.0.0.1:21702".parse().unwrap();
    let discovery_a = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            advertise_a,
            &unique_node_id("redis-discovery-only-a"),
        )
        .unwrap(),
    );
    let discovery_b = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            advertise_b,
            &unique_node_id("redis-discovery-only-b"),
        )
        .unwrap(),
    );

    let peer_client_a = Arc::new(
        PeerClient::new(runtime.clone(), discovery_a.clone()).with_local_addr(advertise_a),
    );
    let peer_client_b = Arc::new(
        PeerClient::new(runtime.clone(), discovery_b.clone()).with_local_addr(advertise_b),
    );

    let server_a = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db_a),
        advertise_a,
        &sock_a,
        Some(peer_client_a),
    )
    .unwrap();
    let server_b = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db_b),
        advertise_b,
        &sock_b,
        Some(peer_client_b),
    )
    .unwrap();

    let shutdown_a = server_a.shutdown_handle();
    let shutdown_b = server_b.shutdown_handle();
    let handle_a = thread::spawn(move || server_a.run().unwrap());
    let handle_b = thread::spawn(move || server_b.run().unwrap());

    wait_until(|| sock_a.exists());
    wait_until(|| sock_b.exists());
    wait_until_with_timeout(Duration::from_secs(10), || {
        discovery_b.get_peers().contains(&advertise_a)
    });

    let local_client_b = LocalChunkClient::new(runtime, &sock_b, Duration::from_millis(500));
    assert!(local_client_b.prefetch_chunk_blocking(&checksum));
    assert_eq!(db_b.get_chunk(&checksum).unwrap().unwrap(), data);

    shutdown_b.shutdown();
    shutdown_a.shutdown();
    discovery_b.shutdown();
    discovery_a.shutdown();
    handle_b.join().unwrap();
    handle_a.join().unwrap();
}

#[test]
fn redis_end_to_end_repair_restores_deleted_owner_and_prefetch_succeeds() {
    let redis = RedisTestGuard::acquire();
    let runtime = PeerRuntime::new().unwrap();
    let server_a_dir = TempDir::new().unwrap();
    let server_b_dir = TempDir::new().unwrap();
    let db_a = Arc::new(ChunkDB::new(server_a_dir.path()).unwrap());
    let db_b = Arc::new(ChunkDB::new(server_b_dir.path()).unwrap());
    let data = b"redis-repair-e2e".to_vec();
    let checksum = CheckSum::from_data(&data, CheckSumMethod::Blake3);
    db_a.add_chunk(&checksum, data.clone()).unwrap();

    let sock_a = server_a_dir.path().join("chunkserver.sock");
    let sock_b = server_b_dir.path().join("chunkserver.sock");
    let advertise_a: SocketAddr = "127.0.0.1:21801".parse().unwrap();
    let advertise_b: SocketAddr = "127.0.0.1:21802".parse().unwrap();
    let discovery_a = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            advertise_a,
            &unique_node_id("redis-repair-e2e-a"),
        )
        .unwrap(),
    );
    let discovery_b = Arc::new(
        RedisDiscovery::new(
            runtime.clone(),
            redis.url(),
            advertise_b,
            &unique_node_id("redis-repair-e2e-b"),
        )
        .unwrap(),
    );
    let chunk_index_a = Arc::new(
        RedisChunkIndex::new(
            redis.url(),
            advertise_a,
            &unique_node_id("redis-repair-e2e-index-a"),
            3,
        )
        .unwrap(),
    );
    let chunk_index_b = Arc::new(
        RedisChunkIndex::new(
            redis.url(),
            advertise_b,
            &unique_node_id("redis-repair-e2e-index-b"),
            3,
        )
        .unwrap(),
    );

    let peer_client_a = Arc::new(
        PeerClient::new(runtime.clone(), discovery_a.clone())
            .with_local_addr(advertise_a)
            .with_chunk_index(chunk_index_a.clone()),
    );
    let peer_client_b = Arc::new(
        PeerClient::new(runtime.clone(), discovery_b.clone())
            .with_local_addr(advertise_b)
            .with_chunk_index(chunk_index_b.clone()),
    );

    let server_a = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db_a),
        advertise_a,
        &sock_a,
        Some(peer_client_a),
    )
    .unwrap();
    let server_b = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db_b),
        advertise_b,
        &sock_b,
        Some(peer_client_b),
    )
    .unwrap();

    let shutdown_a = server_a.shutdown_handle();
    let shutdown_b = server_b.shutdown_handle();
    let handle_a = thread::spawn(move || server_a.run().unwrap());
    let handle_b = thread::spawn(move || server_b.run().unwrap());

    wait_until(|| sock_a.exists());
    wait_until(|| sock_b.exists());
    wait_until_with_timeout(Duration::from_secs(10), || {
        discovery_b.get_peers().contains(&advertise_a)
    });
    wait_until_with_timeout(Duration::from_secs(10), || {
        runtime
            .block_on(chunk_index_b.lookup_owners(&checksum))
            .unwrap()
            .contains(&advertise_a)
    });

    redis_remove_owner(redis.url(), &checksum, advertise_a);
    wait_until_with_timeout(Duration::from_secs(10), || {
        runtime
            .block_on(chunk_index_b.lookup_owners(&checksum))
            .unwrap()
            .is_empty()
    });
    wait_until_with_timeout(Duration::from_secs(10), || {
        runtime
            .block_on(chunk_index_b.lookup_owners(&checksum))
            .unwrap()
            .contains(&advertise_a)
    });

    let local_client_b = LocalChunkClient::new(runtime, &sock_b, Duration::from_millis(500));
    assert!(local_client_b.prefetch_chunk_blocking(&checksum));
    assert_eq!(db_b.get_chunk(&checksum).unwrap().unwrap(), data);

    shutdown_b.shutdown();
    shutdown_a.shutdown();
    discovery_b.shutdown();
    discovery_a.shutdown();
    handle_b.join().unwrap();
    handle_a.join().unwrap();
}
