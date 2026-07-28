#![cfg(target_os = "linux")]

use crate::support::{wait_until, TestChunkIndex};
use imagefsd::backend::chunkdb::{CheckSum, CheckSumMethod, ChunkDB};
use imagefsd::backend::peer::{
    ChunkServer, LocalChunkClient, PeerClient, PeerRuntime, StaticPeers,
};
use std::sync::Arc;
use std::thread;
use std::time::Duration;
use tempfile::TempDir;

#[test]
fn peer_and_local_clients_health_check() {
    let runtime = PeerRuntime::new().unwrap();
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
    let addr = server.local_addr().unwrap();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let discovery = Arc::new(StaticPeers::new(vec![addr]));
    let peer_client = PeerClient::new(runtime.clone(), discovery);
    let local_client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(300));

    assert!(peer_client.health_check_blocking(addr));
    assert!(local_client.health_check_blocking());

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn prefetch_chunk_via_local_client() {
    let runtime = PeerRuntime::new().unwrap();
    let peer_dir = TempDir::new().unwrap();
    let local_dir = TempDir::new().unwrap();
    let peer_db = Arc::new(ChunkDB::new(peer_dir.path()).unwrap());
    let local_db = Arc::new(ChunkDB::new(local_dir.path()).unwrap());
    let data = b"shared-data".to_vec();
    let checksum = CheckSum::from_data(&data, CheckSumMethod::Blake3);
    peer_db.add_chunk(&checksum, data.clone()).unwrap();

    let peer_sock = peer_dir.path().join("chunkserver.sock");
    let peer_server = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&peer_db),
        "127.0.0.1:0".parse().unwrap(),
        &peer_sock,
        None,
    )
    .unwrap();
    let peer_addr = peer_server.local_addr().unwrap();
    let peer_shutdown = peer_server.shutdown_handle();
    let peer_handle = thread::spawn(move || peer_server.run().unwrap());
    wait_until(|| peer_sock.exists());

    let discovery = Arc::new(StaticPeers::new(vec![peer_addr]));
    let client = Arc::new(PeerClient::new(runtime.clone(), discovery));
    let local_sock = local_dir.path().join("chunkserver.sock");
    let local_server = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&local_db),
        "127.0.0.1:0".parse().unwrap(),
        &local_sock,
        Some(client),
    )
    .unwrap();
    let local_shutdown = local_server.shutdown_handle();
    let local_handle = thread::spawn(move || local_server.run().unwrap());
    wait_until(|| local_sock.exists());

    let local_client = LocalChunkClient::new(runtime, &local_sock, Duration::from_millis(300));
    assert!(local_client.prefetch_chunk_blocking(&checksum));
    assert_eq!(local_db.get_chunk(&checksum).unwrap().unwrap(), data);

    local_shutdown.shutdown();
    peer_shutdown.shutdown();
    local_handle.join().unwrap();
    peer_handle.join().unwrap();
}

#[test]
fn peer_client_uses_chunk_index_owners() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let data = b"indexed-peer-data".to_vec();
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
    let owner_addr = server.local_addr().unwrap();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let discovery = Arc::new(StaticPeers::new(Vec::new()));
    let chunk_index = Arc::new(TestChunkIndex::with_owners(vec![owner_addr]));
    let client = PeerClient::new(runtime.clone(), discovery).with_chunk_index(chunk_index);

    assert_eq!(
        runtime.block_on(client.fetch_chunk(&checksum)).unwrap(),
        data
    );

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn local_chunk_client_missing_socket() {
    let runtime = PeerRuntime::new().unwrap();
    let client = LocalChunkClient::new(
        runtime,
        "/tmp/imagefsd-non-existent.sock",
        Duration::from_millis(50),
    );
    let checksum = CheckSum::from_data(b"x", CheckSumMethod::Blake3);
    assert!(!client.prefetch_chunk_blocking(&checksum));
    assert!(!client.health_check_blocking());
    assert!(!client.register_local_chunk(&checksum));
    assert!(!client.unregister_local_chunk(&checksum));
}

#[test]
fn chunk_server_register_unregister_via_local_client() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let sock = dir.path().join("chunkserver.sock");
    let index = Arc::new(TestChunkIndex::default());
    let discovery = Arc::new(StaticPeers::new(Vec::new()));
    let client =
        Arc::new(PeerClient::new(runtime.clone(), discovery).with_chunk_index(index.clone()));
    let server = ChunkServer::new(
        runtime.clone(),
        Arc::clone(&db),
        "127.0.0.1:0".parse().unwrap(),
        &sock,
        Some(client),
    )
    .unwrap();
    let shutdown = server.shutdown_handle();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let checksum = CheckSum::from_data(b"register-me", CheckSumMethod::Blake3);
    let local_client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(300));
    assert!(local_client.register_local_chunk(&checksum));
    assert!(local_client.unregister_local_chunk(&checksum));
    assert_eq!(index.registered(), vec![checksum]);
    assert_eq!(index.unregistered(), vec![checksum]);

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn chunk_server_register_unregister_noop_without_index() {
    let runtime = PeerRuntime::new().unwrap();
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

    let checksum = CheckSum::from_data(b"noop", CheckSumMethod::Blake3);
    let local_client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(300));
    assert!(local_client.register_local_chunk(&checksum));
    assert!(local_client.unregister_local_chunk(&checksum));

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn peer_client_timeout_returns_none() {
    let runtime = PeerRuntime::new().unwrap();
    let discovery = Arc::new(StaticPeers::new(vec!["127.0.0.1:9".parse().unwrap()]));
    let client = PeerClient::new(runtime.clone(), discovery);
    let checksum = CheckSum::from_data(b"x", CheckSumMethod::Blake3);
    assert!(runtime.block_on(client.fetch_chunk(&checksum)).is_none());
    assert!(!client.health_check_blocking("127.0.0.1:9".parse().unwrap()));
}
