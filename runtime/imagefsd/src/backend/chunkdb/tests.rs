use super::*;
use crate::backend::peer::{
    ChunkIndex, ChunkServer, LocalChunkClient, PeerClient, PeerRuntime, StaticPeers,
};
use crate::test_metrics::{histogram_points_f64, sum_points_u64, MetricsHarness};
use async_trait::async_trait;
use std::collections::BTreeMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};
use std::thread;
use tempfile::TempDir;

fn wait_for_access_time(db: &ChunkDB, cs: &CheckSumOnDisk) -> Option<u64> {
    let deadline = now_epoch_secs() + 2;
    loop {
        let rtxn = db.env.read_txn().ok()?;
        if let Ok(Some(access)) = db.access_db.get(&rtxn, cs) {
            return Some(access.secs);
        }
        if now_epoch_secs() >= deadline {
            return None;
        }
        thread::sleep(Duration::from_millis(10));
    }
}

#[derive(Debug, Default)]
struct RecordingChunkIndex {
    registered: Arc<Mutex<Vec<CheckSum>>>,
    unregistered: Arc<Mutex<Vec<CheckSum>>>,
}

impl RecordingChunkIndex {
    fn registered(&self) -> Vec<CheckSum> {
        self.registered.lock().unwrap().clone()
    }

    fn unregistered(&self) -> Vec<CheckSum> {
        self.unregistered.lock().unwrap().clone()
    }
}

impl ChunkIndexControl for RecordingChunkIndex {
    fn register_chunk(&self, checksum: &CheckSum) -> bool {
        self.registered.lock().unwrap().push(*checksum);
        true
    }

    fn register_chunks(&self, checksums: &[CheckSum]) -> bool {
        self.registered.lock().unwrap().extend_from_slice(checksums);
        true
    }

    fn unregister_chunk(&self, checksum: &CheckSum) -> bool {
        self.unregistered.lock().unwrap().push(*checksum);
        true
    }

    fn unregister_chunks(&self, checksums: &[CheckSum]) -> bool {
        self.unregistered
            .lock()
            .unwrap()
            .extend_from_slice(checksums);
        true
    }
}

#[async_trait]
impl ChunkIndex for RecordingChunkIndex {
    async fn lookup_owners(&self, _cs: &CheckSum) -> anyhow::Result<Vec<SocketAddr>> {
        Ok(Vec::new())
    }

    async fn register(&self, _cs: &CheckSum) -> anyhow::Result<()> {
        Ok(())
    }

    async fn register_batch(&self, _checksums: &[CheckSum]) -> anyhow::Result<()> {
        Ok(())
    }

    async fn unregister(&self, cs: &CheckSum) -> anyhow::Result<()> {
        self.unregistered.lock().unwrap().push(*cs);
        Ok(())
    }
}

fn attrs(pairs: &[(&str, &str)]) -> BTreeMap<String, String> {
    pairs
        .iter()
        .map(|(key, value)| ((*key).to_string(), (*value).to_string()))
        .collect()
}

#[test]
fn test_add_chunk_triggers_async_access_update() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let data = b"hello world";
    let cs = CheckSum::from_data(data, CheckSumMethod::Blake3);
    let cs_on_disk = CheckSumOnDisk::from(cs);

    db.add_chunk(&cs, data.to_vec()).unwrap();

    let access = wait_for_access_time(&db, &cs_on_disk);
    assert!(access.is_some());
}

#[test]
fn test_add_chunk_registers_chunk_index_control() {
    let temp = TempDir::new().unwrap();
    let control = Arc::new(RecordingChunkIndex::default());
    let db = ChunkDB::new_with_index_ctl(temp.path(), Some(control.clone())).unwrap();
    let cs = CheckSum::from_data(b"register-one", CheckSumMethod::Blake3);

    db.add_chunk(&cs, b"register-one".to_vec()).unwrap();

    assert_eq!(control.registered(), vec![cs]);
}

#[test]
fn test_add_chunks_registers_chunk_index_control() {
    let temp = TempDir::new().unwrap();
    let control = Arc::new(RecordingChunkIndex::default());
    let db = ChunkDB::new_with_index_ctl(temp.path(), Some(control.clone())).unwrap();
    let cs1 = CheckSum::from_data(b"register-batch-1", CheckSumMethod::Blake3);
    let cs2 = CheckSum::from_data(b"register-batch-2", CheckSumMethod::Sha256);

    db.add_chunks(vec![
        (cs1, b"register-batch-1".to_vec()),
        (cs2, b"register-batch-2".to_vec()),
    ])
    .unwrap();

    assert_eq!(control.registered(), vec![cs1, cs2]);
}

#[test]
fn test_next_chunk_batch_advances_cursor() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let cs1 = CheckSum::from_data(b"cursor-1", CheckSumMethod::Blake3);
    let cs2 = CheckSum::from_data(b"cursor-2", CheckSumMethod::Blake3);
    let cs3 = CheckSum::from_data(b"cursor-3", CheckSumMethod::Sha256);
    db.add_chunks(vec![
        (cs1, b"cursor-1".to_vec()),
        (cs2, b"cursor-2".to_vec()),
        (cs3, b"cursor-3".to_vec()),
    ])
    .unwrap();

    let first = db.next_chunk_batch(None, 2).unwrap();
    assert_eq!(first.len(), 2);

    let second = db.next_chunk_batch(first.last().copied(), 2).unwrap();
    assert_eq!(second.len(), 1);

    let mut all = first;
    all.extend(second);
    all.sort();
    let mut expected = vec![cs1, cs2, cs3];
    expected.sort();
    assert_eq!(all, expected);
}

#[test]
fn test_next_chunk_batch_zero_batch_size_reads_one() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let cs = CheckSum::from_data(b"cursor-zero", CheckSumMethod::Blake3);
    db.add_chunk(&cs, b"cursor-zero".to_vec()).unwrap();

    let batch = db.next_chunk_batch(None, 0).unwrap();
    assert_eq!(batch, vec![cs]);
}

#[test]
fn test_touch_chunk_updates_access_time() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let data = b"touch me";
    let cs = CheckSum::from_data(data, CheckSumMethod::Blake3);
    let cs_on_disk = CheckSumOnDisk::from(cs);

    db.add_chunk(&cs, data.to_vec()).unwrap();
    let first = wait_for_access_time(&db, &cs_on_disk).unwrap();

    thread::sleep(Duration::from_secs(1));
    db.touch_chunk(&cs).unwrap();

    thread::sleep(Duration::from_millis(200));
    let rtxn = db.env.read_txn().unwrap();
    let second = db.access_db.get(&rtxn, &cs_on_disk).unwrap().unwrap().secs;
    assert_eq!(second, first);
}

#[test]
fn test_update_access_respects_min_interval() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let cs = CheckSumOnDisk::from(CheckSum::from_data(b"interval", CheckSumMethod::Blake3));

    let mut wtxn = db.env.write_txn().unwrap();
    ChunkDB::update_access(&db.access_db, &db.access_index, &mut wtxn, &cs, 100).unwrap();
    wtxn.commit().unwrap();

    let mut wtxn = db.env.write_txn().unwrap();
    ChunkDB::update_access(&db.access_db, &db.access_index, &mut wtxn, &cs, 120).unwrap();
    wtxn.commit().unwrap();

    let rtxn = db.env.read_txn().unwrap();
    let access = db.access_db.get(&rtxn, &cs).unwrap().unwrap();
    assert_eq!(access.secs, 100);
    assert!(db
        .access_index
        .get(&rtxn, &AccessKey::new(100, cs))
        .unwrap()
        .is_some());
    assert!(db
        .access_index
        .get(&rtxn, &AccessKey::new(120, cs))
        .unwrap()
        .is_none());
    drop(rtxn);

    let mut wtxn = db.env.write_txn().unwrap();
    ChunkDB::update_access(&db.access_db, &db.access_index, &mut wtxn, &cs, 170).unwrap();
    wtxn.commit().unwrap();

    let rtxn = db.env.read_txn().unwrap();
    let access = db.access_db.get(&rtxn, &cs).unwrap().unwrap();
    assert_eq!(access.secs, 170);
    assert!(db
        .access_index
        .get(&rtxn, &AccessKey::new(170, cs))
        .unwrap()
        .is_some());
}

#[test]
fn test_gc_expired_removes_old_chunks() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let now = now_epoch_secs();

    let cs_old = CheckSum::from_data(b"old", CheckSumMethod::Blake3);
    let cs_new = CheckSum::from_data(b"new", CheckSumMethod::Blake3);
    let cs_old_disk = CheckSumOnDisk::from(cs_old);
    let cs_new_disk = CheckSumOnDisk::from(cs_new);

    let mut wtxn = db.env.write_txn().unwrap();
    db.data_db.put(&mut wtxn, &cs_old_disk, b"old").unwrap();
    db.data_db.put(&mut wtxn, &cs_new_disk, b"new").unwrap();
    ChunkDB::update_access(
        &db.access_db,
        &db.access_index,
        &mut wtxn,
        &cs_old_disk,
        now - 100,
    )
    .unwrap();
    ChunkDB::update_access(
        &db.access_db,
        &db.access_index,
        &mut wtxn,
        &cs_new_disk,
        now - 1,
    )
    .unwrap();
    wtxn.commit().unwrap();

    let removed = db.gc_expired(Duration::from_secs(10)).unwrap();
    assert_eq!(removed.removed, 1);
    assert_eq!(removed.checksums, vec![cs_old]);
    assert!(!db.has_chunk(&cs_old).unwrap());
    assert!(db.has_chunk(&cs_new).unwrap());
}

#[test]
fn test_with_chunk_borrows_full_chunk() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let data = b"with-chunk".to_vec();
    let cs = CheckSum::from_data(&data, CheckSumMethod::Blake3);

    db.add_chunk(&cs, data.clone()).unwrap();

    let got = db
        .with_chunk(&cs, |chunk| {
            Ok(chunk.iter().rev().copied().collect::<Vec<_>>())
        })
        .unwrap();
    let expected = data.iter().rev().copied().collect::<Vec<_>>();
    assert_eq!(got, Some(expected));
}

#[test]
fn test_chunkdb_sets_configured_max_readers() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();

    assert_eq!(db.env.max_readers(), LMDB_MAX_READERS);
}

#[test]
fn test_chunkdb_metrics_emit_get_add_touch_and_gc() {
    let harness = MetricsHarness::new();
    let temp = TempDir::new().unwrap();
    let mut db = ChunkDB::new(temp.path()).unwrap();
    db.metrics = ChunkDbMetrics::new(&harness.meter("imagefsd.chunkdb.test"));
    let data = b"chunkdb-metrics";
    let cs = CheckSum::from_data(data, CheckSumMethod::Blake3);

    db.add_chunk(&cs, data.to_vec()).unwrap();
    assert_eq!(db.get_chunk(&cs).unwrap(), Some(data.to_vec()));
    db.touch_chunk(&cs).unwrap();
    assert!(wait_for_access_time(&db, &CheckSumOnDisk::from(cs)).is_some());
    let removed = db.gc_lru(1).unwrap();
    assert_eq!(removed.removed, 1);

    let collected = harness.collect();
    assert!(sum_points_u64(&collected, "imagefsd.chunkdb.add_total")
        .contains(&(attrs(&[("result", "ok")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.chunkdb.add_bytes")
        .contains(&(attrs(&[("result", "ok")]), data.len() as u64)));
    assert!(sum_points_u64(&collected, "imagefsd.chunkdb.get_total")
        .contains(&(attrs(&[("result", "hit")]), 1)));
    assert!(sum_points_u64(&collected, "imagefsd.chunkdb.get_bytes")
        .contains(&(attrs(&[("result", "hit")]), data.len() as u64)));
    assert!(sum_points_u64(&collected, "imagefsd.chunkdb.touch_total")
        .contains(&(attrs(&[("result", "ok")]), 1)));
    assert!(
        sum_points_u64(&collected, "imagefsd.chunkdb.gc_removed_total")
            .contains(&(attrs(&[("mode", "lru"), ("result", "ok")]), 1))
    );
    assert!(
        histogram_points_f64(&collected, "imagefsd.chunkdb.get_duration_ms")
            .iter()
            .any(
                |(point_attrs, count, _)| point_attrs == &attrs(&[("result", "hit")])
                    && *count == 1
            )
    );
    assert!(
        histogram_points_f64(&collected, "imagefsd.chunkdb.gc_duration_ms")
            .iter()
            .any(|(point_attrs, count, _)| {
                point_attrs == &attrs(&[("mode", "lru"), ("result", "ok")]) && *count == 1
            })
    );
}

#[test]
fn test_gc_lru_removes_oldest_chunks() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();

    let cs1 = CheckSum::from_data(b"chunk1", CheckSumMethod::Blake3);
    let cs2 = CheckSum::from_data(b"chunk2", CheckSumMethod::Blake3);
    let cs3 = CheckSum::from_data(b"chunk3", CheckSumMethod::Blake3);
    let cs1_disk = CheckSumOnDisk::from(cs1);
    let cs2_disk = CheckSumOnDisk::from(cs2);
    let cs3_disk = CheckSumOnDisk::from(cs3);

    let mut wtxn = db.env.write_txn().unwrap();
    db.data_db.put(&mut wtxn, &cs1_disk, b"chunk1").unwrap();
    db.data_db.put(&mut wtxn, &cs2_disk, b"chunk2").unwrap();
    db.data_db.put(&mut wtxn, &cs3_disk, b"chunk3").unwrap();
    ChunkDB::update_access(&db.access_db, &db.access_index, &mut wtxn, &cs1_disk, 1).unwrap();
    ChunkDB::update_access(&db.access_db, &db.access_index, &mut wtxn, &cs2_disk, 2).unwrap();
    ChunkDB::update_access(&db.access_db, &db.access_index, &mut wtxn, &cs3_disk, 3).unwrap();
    wtxn.commit().unwrap();

    let removed = db.gc_lru(2).unwrap();
    assert_eq!(removed.removed, 2);
    assert_eq!(removed.checksums, vec![cs1, cs2]);
    assert!(!db.has_chunk(&cs1).unwrap());
    assert!(!db.has_chunk(&cs2).unwrap());
    assert!(db.has_chunk(&cs3).unwrap());
}

#[test]
fn test_gc_worker_unregisters_deleted_checksums() {
    let temp = TempDir::new().unwrap();
    let server_dir = TempDir::new().unwrap();
    let checksum = CheckSum::from_data(b"gc-worker", CheckSumMethod::Blake3);
    let checksum_disk = CheckSumOnDisk::from(checksum);
    let now = now_epoch_secs();

    {
        let db = ChunkDB::new(temp.path()).unwrap();
        let mut wtxn = db.env.write_txn().unwrap();
        db.data_db
            .put(&mut wtxn, &checksum_disk, b"gc-worker")
            .unwrap();
        ChunkDB::update_access(
            &db.access_db,
            &db.access_index,
            &mut wtxn,
            &checksum_disk,
            now - 100,
        )
        .unwrap();
        wtxn.commit().unwrap();
    }

    let runtime = PeerRuntime::new().unwrap();
    let sock = server_dir.path().join("chunkserver.sock");
    let index = Arc::new(RecordingChunkIndex::default());
    let discovery = Arc::new(StaticPeers::new(Vec::new()));
    let peer_client =
        Arc::new(PeerClient::new(runtime.clone(), discovery).with_chunk_index(index.clone()));
    let server = ChunkServer::new(
        runtime.clone(),
        Arc::new(ChunkDB::new(server_dir.path()).unwrap()),
        "127.0.0.1:0".parse().unwrap(),
        &sock,
        Some(peer_client),
    )
    .unwrap();
    let shutdown = server.shutdown_handle();
    let handle = thread::spawn(move || server.run().unwrap());
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(3);
    while !sock.exists() {
        assert!(
            std::time::Instant::now() < deadline,
            "socket was not created in time"
        );
        thread::sleep(Duration::from_millis(20));
    }

    let local_client = LocalChunkClient::new(runtime, &sock, Duration::from_millis(300));
    let worker = GcWorker::new_with_opts_and_client(
        temp.path(),
        Duration::from_secs(10),
        1.0,
        1.0,
        Some(Arc::new(local_client)),
    )
    .unwrap();
    worker.run(false).unwrap();

    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(3);
    while index.unregistered().is_empty() {
        assert!(
            std::time::Instant::now() < deadline,
            "unregister was not observed in time"
        );
        thread::sleep(Duration::from_millis(20));
    }
    assert_eq!(index.unregistered(), vec![checksum]);

    shutdown.shutdown();
    handle.join().unwrap();
}

#[test]
fn test_get_stats() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();

    let data_blake3_1 = b"blake3 chunk 1";
    let data_blake3_2 = b"blake3 chunk 2 with more data";
    let data_sha256 = b"sha256 chunk";

    let cs_blake3_1 = CheckSum::from_data(data_blake3_1, CheckSumMethod::Blake3);
    let cs_blake3_2 = CheckSum::from_data(data_blake3_2, CheckSumMethod::Blake3);
    let cs_sha256 = CheckSum::from_data(data_sha256, CheckSumMethod::Sha256);

    db.add_chunk(&cs_blake3_1, data_blake3_1.to_vec()).unwrap();
    db.add_chunk(&cs_blake3_2, data_blake3_2.to_vec()).unwrap();
    db.add_chunk(&cs_sha256, data_sha256.to_vec()).unwrap();

    let stats = db.get_stats().unwrap();

    assert!(stats["storage"]["total_size_bytes"].as_u64().unwrap() > 0);
    assert!(stats["storage"]["used_size_bytes"].as_u64().unwrap() > 0);
    assert!(stats["storage"]["free_size_bytes"].as_u64().is_some());

    let total_count = stats["chunks"]["total_count"].as_u64().unwrap();
    assert_eq!(total_count, 3);

    assert!(
        stats["access_time"]["oldest_epoch_secs"].is_null()
            || stats["access_time"]["oldest_epoch_secs"].as_u64().is_some()
    );
    assert!(
        stats["access_time"]["newest_epoch_secs"].is_null()
            || stats["access_time"]["newest_epoch_secs"].as_u64().is_some()
    );
}

#[test]
fn test_list_all_chunks() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();

    let cs1 = CheckSum::from_data(b"list-1", CheckSumMethod::Blake3);
    let cs2 = CheckSum::from_data(b"list-2", CheckSumMethod::Sha256);
    db.add_chunk(&cs1, b"list-1".to_vec()).unwrap();
    db.add_chunk(&cs2, b"list-2".to_vec()).unwrap();

    let mut checksums = db.list_all_chunks().unwrap();
    checksums.sort();

    let mut expected = vec![cs1, cs2];
    expected.sort();
    assert_eq!(checksums, expected);
}

#[test]
fn test_writer_thread_prioritizes_add_chunk_over_access_updates() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let cs = CheckSum::from_data(b"priority-test", CheckSumMethod::Blake3);

    db.add_chunk(&cs, b"priority-test".to_vec()).unwrap();

    // High-priority result: chunk is immediately readable after add_chunk returns
    assert_eq!(db.get_chunk(&cs).unwrap(), Some(b"priority-test".to_vec()));
    // Low-priority: access update arrives asynchronously
    assert!(wait_for_access_time(&db, &CheckSumOnDisk::from(cs)).is_some());
}

#[test]
fn test_add_chunks_large_batch_sub_batching() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();

    let chunks: Vec<(CheckSum, Vec<u8>)> = (0..100)
        .map(|i| {
            let data = format!("sub-batch-{i:04}").into_bytes();
            (CheckSum::from_data(&data, CheckSumMethod::Blake3), data)
        })
        .collect();
    let checksums: Vec<CheckSum> = chunks.iter().map(|(cs, _)| *cs).collect();

    db.add_chunks(chunks).unwrap();

    for cs in &checksums {
        assert!(
            db.has_chunk(cs).unwrap(),
            "missing chunk after large batch add"
        );
    }
}

#[test]
fn test_add_chunks_registers_per_sub_batch() {
    let temp = TempDir::new().unwrap();
    let control = Arc::new(RecordingChunkIndex::default());
    let db = ChunkDB::new_with_index_ctl(temp.path(), Some(control.clone())).unwrap();

    // 40 chunks = 3 sub-batches (16+16+8) with ADD_CHUNKS_BATCH_SIZE=16
    let chunks: Vec<(CheckSum, Vec<u8>)> = (0..40)
        .map(|i| {
            let data = format!("reg-sub-{i:04}").into_bytes();
            (CheckSum::from_data(&data, CheckSumMethod::Blake3), data)
        })
        .collect();
    let expected: Vec<CheckSum> = chunks.iter().map(|(cs, _)| *cs).collect();

    db.add_chunks(chunks).unwrap();

    let mut registered = control.registered();
    registered.sort();
    let mut expected_sorted = expected;
    expected_sorted.sort();
    assert_eq!(registered, expected_sorted);
}

#[test]
fn test_gc_expired_large_batch_batched_deletion() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let now = now_epoch_secs();

    let mut wtxn = db.env.write_txn().unwrap();
    let mut checksums = Vec::new();
    for i in 0..300u32 {
        let data = format!("gc-batch-{i:04}").into_bytes();
        let cs = CheckSum::from_data(&data, CheckSumMethod::Blake3);
        let csd = CheckSumOnDisk::from(cs);
        db.data_db.put(&mut wtxn, &csd, &data).unwrap();
        ChunkDB::update_access(&db.access_db, &db.access_index, &mut wtxn, &csd, now - 1000)
            .unwrap();
        checksums.push(cs);
    }
    wtxn.commit().unwrap();

    let result = db.gc_expired(Duration::from_secs(100)).unwrap();
    assert_eq!(result.removed, 300);
    for cs in &checksums {
        assert!(!db.has_chunk(cs).unwrap());
    }
}

#[test]
fn test_gc_lru_through_writer_channel() {
    let temp = TempDir::new().unwrap();
    let control = Arc::new(RecordingChunkIndex::default());
    let db = ChunkDB::new_with_index_ctl(temp.path(), Some(control.clone())).unwrap();

    let cs1 = CheckSum::from_data(b"gc-writer-1", CheckSumMethod::Blake3);
    let cs2 = CheckSum::from_data(b"gc-writer-2", CheckSumMethod::Blake3);
    db.add_chunk(&cs1, b"gc-writer-1".to_vec()).unwrap();
    db.add_chunk(&cs2, b"gc-writer-2".to_vec()).unwrap();

    // Wait for access updates so gc_lru can find them
    assert!(wait_for_access_time(&db, &CheckSumOnDisk::from(cs1)).is_some());
    assert!(wait_for_access_time(&db, &CheckSumOnDisk::from(cs2)).is_some());

    // gc_lru goes through delete_keys → medium priority in WriterThread
    let result = db.gc_lru(2).unwrap();
    assert_eq!(result.removed, 2);
    assert!(!db.has_chunk(&cs1).unwrap());
    assert!(!db.has_chunk(&cs2).unwrap());
}

#[test]
fn test_get_stats_includes_readers_field() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let stats = db.get_stats().unwrap();

    assert!(stats["readers"]["current"].as_u64().is_some());
    assert_eq!(
        stats["readers"]["max"].as_u64().unwrap(),
        LMDB_MAX_READERS as u64
    );
    assert!(stats["readers"]["stale_cleared"].as_u64().is_some());
}

#[test]
fn test_clear_stale_readers_returns_ok() {
    let temp = TempDir::new().unwrap();
    let db = ChunkDB::new(temp.path()).unwrap();
    let cleared = db.clear_stale_readers().unwrap();
    assert_eq!(cleared, 0);
}
