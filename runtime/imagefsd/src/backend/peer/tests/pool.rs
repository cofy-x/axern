use super::*;
use tokio::net::UnixStream;

#[test]
fn test_multiplexed_session_touch_updates_last_used() {
    let runtime = PeerRuntime::new().unwrap();
    runtime.block_on(async {
        let (stream, _peer) = tokio::io::duplex(64);
        let (reader, writer) = split(stream);
        let session = MultiplexedSession::new(reader, Box::new(writer));
        let before = session.last_used();
        tokio::time::sleep(Duration::from_millis(2)).await;
        session.touch();
        assert!(session.last_used() >= before);
    });
}

#[test]
fn test_peer_client_reuses_tcp_connection() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let data = b"reused-peer-data".to_vec();
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
    let client =
        PeerClient::new(runtime.clone(), discovery).with_timeout(Duration::from_millis(300));
    let pool = Arc::new(TcpConnPool::with_config(short_pool_config()));
    runtime.block_on(async {
        client.pools.lock().await.insert(addr, Arc::clone(&pool));
    });

    assert_eq!(client.fetch_chunk_blocking(&checksum).unwrap(), data);
    assert_eq!(client.fetch_chunk_blocking(&checksum).unwrap(), data);
    assert_eq!(pool.connect_count(), 1);

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_local_chunk_client_reuses_unix_connection() {
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

    let checksum = CheckSum::from_data(b"reuse-local", CheckSumMethod::Blake3);
    let mut client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(300));
    client.pool = Arc::new(UnixConnPool::with_config(short_pool_config()));

    assert!(client.register_local_chunk(&checksum));
    assert!(client.unregister_local_chunk(&checksum));
    assert_eq!(client.pool.connect_count(), 1);

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_local_chunk_client_prunes_idle_connections_to_minimum() {
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

    let mut client = LocalChunkClient::new(runtime.clone(), &sock, Duration::from_millis(300));
    client.pool = Arc::new(UnixConnPool::with_config(short_pool_config()));

    runtime.block_on(async {
        let first = client
            .pool
            .acquire(|| async {
                let stream = UnixStream::connect(&client.socket_path).await?;
                Ok(MultiplexedSession::from_unix(stream))
            })
            .await
            .unwrap();
        first
            .inflight
            .store(SESSION_MAX_INFLIGHT, Ordering::Relaxed);
        let second = client
            .pool
            .acquire(|| async {
                let stream = UnixStream::connect(&client.socket_path).await?;
                Ok(MultiplexedSession::from_unix(stream))
            })
            .await
            .unwrap();
        first.inflight.store(0, Ordering::Relaxed);
        drop(first);
        drop(second);
        client.pool.prune().await;
    });
    assert_eq!(runtime.block_on(client.pool.state_counts()), (2, 2));

    thread::sleep(Duration::from_millis(80));

    runtime.block_on(async {
        let conn = client
            .pool
            .acquire(|| async {
                let stream = UnixStream::connect(&client.socket_path).await?;
                Ok(MultiplexedSession::from_unix(stream))
            })
            .await
            .unwrap();
        drop(conn);
        client.pool.prune().await;
    });
    assert_eq!(runtime.block_on(client.pool.state_counts()), (1, 1));

    shutdown.shutdown();
    handle.join().unwrap();
}
