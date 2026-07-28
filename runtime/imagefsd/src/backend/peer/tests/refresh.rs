use super::*;

#[test]
fn test_refresh_batch_spacing_reserves_headroom() {
    assert_eq!(
        RedisChunkIndex::refresh_batch_spacing(Duration::from_secs(20), 50),
        Duration::from_millis(360)
    );
    assert_eq!(
        RedisChunkIndex::refresh_batch_spacing(Duration::from_secs(20), 1),
        Duration::ZERO
    );
}

#[test]
fn test_refresh_index_passes_spread_over_to_chunk_index() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let chunk_db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let chunk_index = Arc::new(TestChunkIndex {
        refresh_result: Some(7),
        ..Default::default()
    });

    let refreshed = runtime
        .block_on(ChunkServer::refresh_index(
            chunk_db,
            chunk_index.clone(),
            Duration::from_secs(12),
        ))
        .unwrap();

    assert_eq!(refreshed, 7);
    assert!(chunk_index.registered().is_empty());
    assert_eq!(chunk_index.refresh_spreads(), vec![Duration::from_secs(12)]);
}

#[test]
fn test_refresh_index_falls_back_to_chunk_scan() {
    let runtime = PeerRuntime::new().unwrap();
    let dir = TempDir::new().unwrap();
    let chunk_db = Arc::new(ChunkDB::new(dir.path()).unwrap());
    let first_data = b"refresh-first".to_vec();
    let second_data = b"refresh-second".to_vec();
    let first = CheckSum::from_data(&first_data, CheckSumMethod::Blake3);
    let second = CheckSum::from_data(&second_data, CheckSumMethod::Blake3);
    chunk_db.add_chunk(&first, first_data).unwrap();
    chunk_db.add_chunk(&second, second_data).unwrap();
    let chunk_index = Arc::new(TestChunkIndex::default());

    let refreshed = runtime
        .block_on(ChunkServer::refresh_index(
            Arc::clone(&chunk_db),
            chunk_index.clone(),
            Duration::from_secs(9),
        ))
        .unwrap();

    let registered = chunk_index.registered();
    assert_eq!(refreshed, 2);
    assert_eq!(registered.len(), 2);
    assert!(registered.contains(&first));
    assert!(registered.contains(&second));
    assert_eq!(chunk_index.refresh_spreads(), vec![Duration::from_secs(9)]);
}

#[test]
fn test_index_tracker_tracks_and_removes_checksums() {
    let tracker = IndexTracker::default();
    let a = CheckSum::from_data(b"tracker-a", CheckSumMethod::Blake3);
    let b = CheckSum::from_data(b"tracker-b", CheckSumMethod::Sha256);

    tracker.insert_many([CheckSumOnDisk::from(a), CheckSumOnDisk::from(b)]);
    assert!(tracker.contains(&a));
    assert!(tracker.contains(&b));

    let mut snapshot = tracker.snapshot();
    snapshot.sort();
    let mut expected = vec![a, b];
    expected.sort();
    assert_eq!(snapshot, expected);

    tracker.remove_many([CheckSumOnDisk::from(a)]);
    assert!(!tracker.contains(&a));
    assert!(tracker.contains(&b));
}
