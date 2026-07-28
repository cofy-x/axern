use super::*;
use serde_json::Value;
use tokio::io::duplex;

#[test]
fn test_request_roundtrip() {
    let checksum = CheckSum::from_data(b"hello", CheckSumMethod::Blake3);
    let req = Request::whole_chunk(MessageType::GetChunk, checksum);
    let encoded = req.encode();
    let decoded = Request::decode(encoded).unwrap();
    assert_eq!(decoded, req);
}

#[test]
fn test_request_rejects_non_full_chunk() {
    let checksum = CheckSum::from_data(b"hello", CheckSumMethod::Blake3);
    let req = Request::new(0, MessageType::GetChunk, checksum, 12, 34);
    let err = req.ensure_full_chunk().unwrap_err();
    assert_eq!(err.kind(), ErrorKind::InvalidInput);
}

#[test]
fn test_chunk_response_rejects_oversized_payload() {
    let mut data = Vec::new();
    data.extend_from_slice(&0_u64.to_be_bytes());
    data.push(STATUS_HIT);
    data.extend_from_slice(&((MAX_CHUNK_PAYLOAD_SIZE as u32) + 1).to_be_bytes());
    let err = WireResponse::read_from_sync(&mut data.as_slice()).unwrap_err();
    assert_eq!(err.kind(), ErrorKind::InvalidData);
}

#[test]
fn test_wire_response_roundtrip() {
    let runtime = PeerRuntime::new().unwrap();
    runtime.block_on(async {
        let response = WireResponse {
            request_id: 42,
            status: STATUS_HIT,
            payload: b"payload".to_vec(),
        };
        let (mut writer, mut reader) = tokio::io::duplex(64);
        let write_task = tokio::spawn(async move { response.write_to(&mut writer).await });
        let decoded = WireResponse::read_from(&mut reader).await.unwrap();
        write_task.await.unwrap().unwrap();
        assert_eq!(decoded.request_id, 42);
        assert_eq!(decoded.status, STATUS_HIT);
        assert_eq!(decoded.payload, b"payload".to_vec());
    });
}

#[test]
fn test_chunk_server_get_chunk() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let data = b"peer-data".to_vec();
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

    let mut stream = StdTcpStream::connect(addr).unwrap();
    Request::whole_chunk(MessageType::GetChunk, checksum)
        .write_to_sync(&mut stream)
        .unwrap();
    let response = WireResponse::read_from_sync(&mut stream).unwrap();
    assert_eq!(response.status, STATUS_HIT);
    assert_eq!(response.payload, data);

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_chunk_server_keeps_tcp_connection_alive_for_multiple_requests() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let data = b"peer-data".to_vec();
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

    let mut stream = StdTcpStream::connect(addr).unwrap();
    for _ in 0..2 {
        Request::whole_chunk(MessageType::GetChunk, checksum)
            .write_to_sync(&mut stream)
            .unwrap();
        let response = WireResponse::read_from_sync(&mut stream).unwrap();
        assert_eq!(response.status, STATUS_HIT);
        assert_eq!(response.payload, data);
    }

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_chunk_server_raw_pipelined_requests_require_request_id_matching() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let data = b"peer-data".to_vec();
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

    let mut stream = StdTcpStream::connect(addr).unwrap();
    let get_request = Request::whole_chunk(MessageType::GetChunk, checksum);
    let health_request = Request::whole_chunk(MessageType::HealthCheck, CheckSum::empty());
    get_request.write_to_sync(&mut stream).unwrap();
    health_request.write_to_sync(&mut stream).unwrap();

    let first = WireResponse::read_from_sync(&mut stream).unwrap();
    let second = WireResponse::read_from_sync(&mut stream).unwrap();
    let responses = [first, second];

    assert!(responses.iter().any(|response| {
        response.request_id == get_request.request_id
            && response.status == STATUS_HIT
            && response.payload == data
    }));
    assert!(responses.iter().any(|response| {
        response.request_id == health_request.request_id
            && response.status == STATUS_HIT
            && response.payload.is_empty()
    }));

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_chunk_server_health_check_over_tcp_and_unix() {
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
    let addr = server.tcp_listener.local_addr().unwrap();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let mut tcp_stream = StdTcpStream::connect(addr).unwrap();
    Request::whole_chunk(MessageType::HealthCheck, CheckSum::empty())
        .write_to_sync(&mut tcp_stream)
        .unwrap();
    let tcp_resp = WireResponse::read_from_sync(&mut tcp_stream).unwrap();
    assert_eq!(tcp_resp.status, STATUS_HIT);
    assert!(tcp_resp.payload.is_empty());

    let mut unix_stream = StdUnixStream::connect(&sock).unwrap();
    Request::whole_chunk(MessageType::HealthCheck, CheckSum::empty())
        .write_to_sync(&mut unix_stream)
        .unwrap();
    let unix_resp = WireResponse::read_from_sync(&mut unix_stream).unwrap();
    assert_eq!(unix_resp.status, STATUS_HIT);
    assert!(unix_resp.payload.is_empty());

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_chunk_server_stats_locality_is_unix_only() {
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
    let addr = server.tcp_listener.local_addr().unwrap();
    let handle = thread::spawn(move || server.run().unwrap());
    wait_until(|| sock.exists());

    let mut tcp_stream = StdTcpStream::connect(addr).unwrap();
    Request::whole_chunk(MessageType::StatsLocality, CheckSum::empty())
        .write_to_sync(&mut tcp_stream)
        .unwrap();
    let tcp_resp = WireResponse::read_from_sync(&mut tcp_stream).unwrap();
    assert_eq!(tcp_resp.status, STATUS_ERROR);
    assert!(tcp_resp.payload.is_empty());

    let mut unix_stream = StdUnixStream::connect(&sock).unwrap();
    Request::whole_chunk(MessageType::StatsLocality, CheckSum::empty())
        .write_to_sync(&mut unix_stream)
        .unwrap();
    let unix_resp = WireResponse::read_from_sync(&mut unix_stream).unwrap();
    assert_eq!(unix_resp.status, STATUS_HIT);
    let payload: Value = serde_json::from_slice(&unix_resp.payload).unwrap();
    assert_eq!(payload["chunkdb_total_chunks"].as_u64(), Some(0),);
    assert_eq!(payload["peer_healthy_count"].as_u64(), Some(0),);

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_chunk_server_keeps_unix_connection_alive_for_multiple_requests() {
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
    let mut stream = StdUnixStream::connect(&sock).unwrap();
    for message_type in [
        MessageType::HealthCheck,
        MessageType::RegisterChunk,
        MessageType::UnregisterChunk,
    ] {
        Request::whole_chunk(message_type, checksum)
            .write_to_sync(&mut stream)
            .unwrap();
        let resp = WireResponse::read_from_sync(&mut stream).unwrap();
        assert_eq!(resp.status, STATUS_HIT);
        assert!(resp.payload.is_empty());
    }

    assert_eq!(index.registered(), vec![checksum]);
    assert_eq!(index.unregistered(), vec![checksum]);

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_chunk_response_roundtrip_preserves_request_id() {
    let runtime = PeerRuntime::new().unwrap();
    runtime.block_on(async {
        let (mut writer, mut reader) = duplex(1024);
        let expected = WireResponse {
            request_id: 42,
            status: STATUS_HIT,
            payload: b"payload".to_vec(),
        };

        let write_task = tokio::spawn({
            let expected = expected.clone();
            async move { expected.write_to(&mut writer).await.unwrap() }
        });
        let actual = WireResponse::read_from(&mut reader).await.unwrap();
        write_task.await.unwrap();

        assert_eq!(actual, expected);
    });
}

#[test]
fn test_multiplexed_session_routes_out_of_order_responses() {
    let runtime = PeerRuntime::new().unwrap();
    runtime.block_on(async {
        let (client_side, mut server_side) = duplex(4096);
        let (reader, writer) = split(client_side);
        let session = MultiplexedSession::new(reader, Box::new(writer));

        let server = tokio::spawn(async move {
            let first = Request::read_from(&mut server_side).await.unwrap();
            let second = Request::read_from(&mut server_side).await.unwrap();
            assert_ne!(first.request_id, second.request_id);

            WireResponse {
                request_id: second.request_id,
                status: STATUS_HIT,
                payload: b"second".to_vec(),
            }
            .write_to(&mut server_side)
            .await
            .unwrap();

            WireResponse {
                request_id: first.request_id,
                status: STATUS_HIT,
                payload: b"first".to_vec(),
            }
            .write_to(&mut server_side)
            .await
            .unwrap();
        });

        let checksum1 = CheckSum::from_data(b"first", CheckSumMethod::Blake3);
        let checksum2 = CheckSum::from_data(b"second", CheckSumMethod::Blake3);
        let timeout = Duration::from_millis(500);
        let send1 = session.send_request(
            Request::whole_chunk(MessageType::GetChunk, checksum1),
            timeout,
        );
        let send2 = session.send_request(
            Request::whole_chunk(MessageType::GetChunk, checksum2),
            timeout,
        );

        let (resp1, resp2) = tokio::join!(send1, send2);
        assert_eq!(resp1.unwrap().payload, b"first".to_vec());
        assert_eq!(resp2.unwrap().payload, b"second".to_vec());
        assert_eq!(session.inflight(), 0);

        server.await.unwrap();
    });
}
