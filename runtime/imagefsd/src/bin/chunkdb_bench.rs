use anyhow::{Context, Result};
use clap::{Parser, ValueEnum};
use imagefsd::backend::chunkdb::{CheckSum, CheckSumMethod, ChunkDB};
use serde_json::{json, Value};
use std::fs;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::Arc;
use std::thread;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

const DEFAULT_CHUNK_SIZE: usize = 4 * 1024 * 1024;
const LATENCY_SAMPLE_CAP: usize = 50_000;
const WORKER_PREFIX: &str = "WORKER_METRICS ";

#[derive(Copy, Clone, Debug, Eq, PartialEq, ValueEnum)]
enum Scenario {
    Write,
    Read,
    Mixed,
}

#[derive(Copy, Clone, Debug, Eq, PartialEq, ValueEnum)]
enum HashMethod {
    Blake3,
    Sha256,
}

impl From<HashMethod> for CheckSumMethod {
    fn from(value: HashMethod) -> Self {
        match value {
            HashMethod::Blake3 => CheckSumMethod::Blake3,
            HashMethod::Sha256 => CheckSumMethod::Sha256,
        }
    }
}

#[derive(Parser, Debug, Clone)]
#[command(name = "chunkdb_bench")]
#[command(about = "ChunkDB micro benchmark (single or multi-process)")]
struct Opts {
    #[arg(long)]
    db_path: String,

    #[arg(long, value_enum, default_value_t = Scenario::Write)]
    scenario: Scenario,

    #[arg(long, default_value_t = DEFAULT_CHUNK_SIZE)]
    chunk_size: usize,

    #[arg(long, default_value_t = 10)]
    duration_secs: u64,

    #[arg(long, default_value_t = 1000)]
    preload_chunks: u64,

    #[arg(long, default_value_t = 1)]
    processes: u32,

    #[arg(long, default_value_t = 30)]
    write_percent: u8,

    #[arg(long, default_value_t = 200_000)]
    write_keyspace_chunks: u64,

    #[arg(long, default_value_t = false)]
    reset_db: bool,

    #[arg(long, value_enum, default_value_t = HashMethod::Blake3)]
    hash_method: HashMethod,

    #[arg(long, default_value_t = false)]
    read_copy: bool,

    #[arg(long, default_value_t = false)]
    mixed_concurrent: bool,

    #[arg(long, default_value_t = 1)]
    mixed_reader_threads: u32,

    #[arg(long, default_value_t = 1)]
    mixed_writer_threads: u32,

    #[arg(long, hide = true)]
    worker_index: Option<u32>,

    #[arg(long, hide = true)]
    run_salt: Option<u64>,

    #[arg(long, hide = true, default_value_t = false)]
    json_only: bool,

    #[arg(long, hide = true, default_value_t = false)]
    skip_preload: bool,
}

#[derive(Debug)]
struct Lcg {
    state: u64,
}

impl Lcg {
    fn new(seed: u64) -> Self {
        let init = if seed == 0 { 0x9E3779B97F4A7C15 } else { seed };
        Self { state: init }
    }

    fn next_u64(&mut self) -> u64 {
        // xorshift64*
        let mut x = self.state;
        x ^= x >> 12;
        x ^= x << 25;
        x ^= x >> 27;
        self.state = x;
        x.wrapping_mul(0x2545F4914F6CDD1D)
    }
}

#[derive(Debug)]
struct LatencySampler {
    cap: usize,
    seen: u64,
    samples: Vec<u64>,
    rng: Lcg,
}

impl LatencySampler {
    fn new(cap: usize, seed: u64) -> Self {
        Self {
            cap,
            seen: 0,
            samples: Vec::with_capacity(cap),
            rng: Lcg::new(seed),
        }
    }

    fn observe(&mut self, ns: u64) {
        self.seen = self.seen.saturating_add(1);
        if self.samples.len() < self.cap {
            self.samples.push(ns);
            return;
        }
        let slot = self.rng.next_u64() % self.seen;
        if slot < self.cap as u64 {
            self.samples[slot as usize] = ns;
        }
    }
}

#[derive(Debug)]
struct WorkerMetrics {
    worker_index: u32,
    process_count: u32,
    elapsed_ns: u128,
    ops_total: u64,
    read_ops: u64,
    write_ops: u64,
    read_misses: u64,
    bytes_read: u64,
    bytes_written: u64,
    latency_seen: u64,
    latency_samples: Vec<u64>,
}

#[derive(Debug, Default)]
struct ThreadMetrics {
    ops_total: u64,
    read_ops: u64,
    write_ops: u64,
    read_misses: u64,
    bytes_read: u64,
    bytes_written: u64,
    latency_seen: u64,
    latency_samples: Vec<u64>,
}

impl WorkerMetrics {
    fn to_json(&self) -> Value {
        let elapsed_secs = ns_to_secs(self.elapsed_ns);
        let mut samples = self.latency_samples.clone();
        samples.sort_unstable();
        json!({
            "worker_index": self.worker_index,
            "process_count": self.process_count,
            "elapsed_ns": self.elapsed_ns,
            "elapsed_secs": elapsed_secs,
            "ops_total": self.ops_total,
            "read_ops": self.read_ops,
            "write_ops": self.write_ops,
            "read_misses": self.read_misses,
            "bytes_read": self.bytes_read,
            "bytes_written": self.bytes_written,
            "ops_per_sec": rate(self.ops_total, elapsed_secs),
            "read_ops_per_sec": rate(self.read_ops, elapsed_secs),
            "write_ops_per_sec": rate(self.write_ops, elapsed_secs),
            "mb_read_per_sec": bytes_rate_mb(self.bytes_read, elapsed_secs),
            "mb_write_per_sec": bytes_rate_mb(self.bytes_written, elapsed_secs),
            "latency_seen": self.latency_seen,
            "latency_sample_count": samples.len(),
            "latency_ns_p50": percentile(&samples, 0.50),
            "latency_ns_p95": percentile(&samples, 0.95),
            "latency_ns_p99": percentile(&samples, 0.99),
        })
    }
}

fn ns_to_secs(ns: u128) -> f64 {
    (ns as f64) / 1_000_000_000.0
}

fn rate(v: u64, secs: f64) -> f64 {
    if secs <= 0.0 {
        0.0
    } else {
        (v as f64) / secs
    }
}

fn bytes_rate_mb(bytes: u64, secs: f64) -> f64 {
    if secs <= 0.0 {
        0.0
    } else {
        (bytes as f64) / secs / (1024.0 * 1024.0)
    }
}

fn percentile(sorted: &[u64], p: f64) -> u64 {
    if sorted.is_empty() {
        return 0;
    }
    let n = sorted.len() as f64;
    let idx = ((n * p).ceil() as usize)
        .saturating_sub(1)
        .min(sorted.len() - 1);
    sorted[idx]
}

fn checksum_for_key(salt: u64, key_idx: u64, method: CheckSumMethod) -> CheckSum {
    let mut seed = [0_u8; 16];
    seed[..8].copy_from_slice(&salt.to_le_bytes());
    seed[8..].copy_from_slice(&key_idx.to_le_bytes());
    CheckSum::from_data(&seed, method)
}

fn prepare_data(chunk_size: usize, worker_index: u32) -> Vec<u8> {
    let mut buf = vec![0_u8; chunk_size];
    let base = (worker_index % 251) as u8;
    for (i, v) in buf.iter_mut().enumerate() {
        *v = base.wrapping_add((i % 251) as u8);
    }
    buf
}

fn preload_for_reads(db: &ChunkDB, opts: &Opts, salt: u64) -> Result<()> {
    if !matches!(opts.scenario, Scenario::Read | Scenario::Mixed) {
        return Ok(());
    }
    let method: CheckSumMethod = opts.hash_method.into();
    let data = prepare_data(opts.chunk_size, 0);
    for idx in 0..opts.preload_chunks {
        let cs = checksum_for_key(salt, idx, method);
        db.add_chunk(&cs, data.clone())?;
    }
    Ok(())
}

fn read_once(
    db: &ChunkDB,
    cs: &CheckSum,
    chunk_size: usize,
    read_copy: bool,
    read_buf: &mut [u8],
) -> Result<bool> {
    let got = db.with_chunk_range(cs, 0, chunk_size, |slice| {
        if read_copy {
            read_buf[..slice.len()].copy_from_slice(slice);
        } else {
            let _ = slice.first();
        }
        Ok(())
    })?;
    Ok(got.is_some())
}

fn run_worker(
    db: Arc<ChunkDB>,
    opts: &Opts,
    worker_index: u32,
    process_count: u32,
    salt: u64,
) -> Result<WorkerMetrics> {
    if opts.scenario == Scenario::Mixed && opts.mixed_concurrent {
        return run_worker_mixed_concurrent(db, opts, worker_index, process_count, salt);
    }
    run_worker_single(db.as_ref(), opts, worker_index, process_count, salt)
}

fn run_worker_single(
    db: &ChunkDB,
    opts: &Opts,
    worker_index: u32,
    process_count: u32,
    salt: u64,
) -> Result<WorkerMetrics> {
    if opts.write_percent > 100 {
        anyhow::bail!("write_percent must be in [0, 100]");
    }
    fs::create_dir_all(&opts.db_path)
        .with_context(|| format!("failed to create db directory: {}", opts.db_path))?;
    let method: CheckSumMethod = opts.hash_method.into();
    let mut rng = Lcg::new(salt ^ ((worker_index as u64) << 32));
    let mut lat = LatencySampler::new(LATENCY_SAMPLE_CAP, rng.next_u64());
    let write_buf = prepare_data(opts.chunk_size, worker_index);

    let preload_base = opts.preload_chunks;
    let write_base = preload_base
        .saturating_add((worker_index as u64) << 40)
        .saturating_add(1);

    let start = Instant::now();
    let deadline = start + std::time::Duration::from_secs(opts.duration_secs);

    let mut write_seq: u64 = 0;
    let mut ops_total: u64 = 0;
    let mut read_ops: u64 = 0;
    let mut write_ops: u64 = 0;
    let mut read_misses: u64 = 0;
    let mut bytes_read: u64 = 0;
    let mut bytes_written: u64 = 0;
    let mut read_buf = if opts.read_copy {
        vec![0_u8; opts.chunk_size]
    } else {
        Vec::new()
    };

    while Instant::now() < deadline {
        let do_write = match opts.scenario {
            Scenario::Write => true,
            Scenario::Read => false,
            Scenario::Mixed => (rng.next_u64() % 100) < opts.write_percent as u64,
        };

        let op_start = Instant::now();
        if do_write {
            let key_idx = if opts.write_keyspace_chunks == 0 {
                write_base.saturating_add(write_seq)
            } else {
                let slot = write_seq % opts.write_keyspace_chunks;
                write_base.saturating_add(slot)
            };
            let cs = checksum_for_key(salt, key_idx, method);
            db.add_chunk(&cs, write_buf.clone())?;
            write_seq = write_seq.saturating_add(1);
            write_ops = write_ops.saturating_add(1);
            bytes_written = bytes_written.saturating_add(opts.chunk_size as u64);
        } else {
            let key_span = opts.preload_chunks.max(1);
            let key_idx = rng.next_u64() % key_span;
            let cs = checksum_for_key(salt, key_idx, method);
            let got = read_once(db, &cs, opts.chunk_size, opts.read_copy, &mut read_buf)?;
            read_ops = read_ops.saturating_add(1);
            if got {
                bytes_read = bytes_read.saturating_add(opts.chunk_size as u64);
            } else {
                read_misses = read_misses.saturating_add(1);
            }
        }

        let op_ns = op_start.elapsed().as_nanos() as u64;
        lat.observe(op_ns);
        ops_total = ops_total.saturating_add(1);
    }

    Ok(WorkerMetrics {
        worker_index,
        process_count,
        elapsed_ns: start.elapsed().as_nanos(),
        ops_total,
        read_ops,
        write_ops,
        read_misses,
        bytes_read,
        bytes_written,
        latency_seen: lat.seen,
        latency_samples: lat.samples,
    })
}

fn run_worker_mixed_concurrent(
    db: Arc<ChunkDB>,
    opts: &Opts,
    worker_index: u32,
    process_count: u32,
    salt: u64,
) -> Result<WorkerMetrics> {
    let method: CheckSumMethod = opts.hash_method.into();
    let chunk_size = opts.chunk_size;
    let preload_chunks = opts.preload_chunks;
    let read_copy = opts.read_copy;
    let reader_threads = opts.mixed_reader_threads;
    let writer_threads = opts.mixed_writer_threads;
    let write_keyspace_chunks = opts.write_keyspace_chunks;

    let start = Instant::now();
    let deadline = start + std::time::Duration::from_secs(opts.duration_secs);
    let mut handles = Vec::new();

    for t in 0..reader_threads {
        let db = Arc::clone(&db);
        let seed = salt ^ ((worker_index as u64) << 32) ^ ((t as u64) << 8) ^ 0xA5A5_A5A5;
        handles.push(thread::spawn(move || -> Result<ThreadMetrics> {
            let mut rng = Lcg::new(seed);
            let mut lat = LatencySampler::new(LATENCY_SAMPLE_CAP, rng.next_u64());
            let mut m = ThreadMetrics::default();
            let mut read_buf = if read_copy {
                vec![0_u8; chunk_size]
            } else {
                Vec::new()
            };
            let key_span = preload_chunks.max(1);

            while Instant::now() < deadline {
                let key_idx = rng.next_u64() % key_span;
                let cs = checksum_for_key(salt, key_idx, method);
                let op_start = Instant::now();
                let hit = read_once(db.as_ref(), &cs, chunk_size, read_copy, &mut read_buf)?;
                let op_ns = op_start.elapsed().as_nanos() as u64;
                lat.observe(op_ns);

                m.ops_total = m.ops_total.saturating_add(1);
                m.read_ops = m.read_ops.saturating_add(1);
                if hit {
                    m.bytes_read = m.bytes_read.saturating_add(chunk_size as u64);
                } else {
                    m.read_misses = m.read_misses.saturating_add(1);
                }
            }

            m.latency_seen = lat.seen;
            m.latency_samples = lat.samples;
            Ok(m)
        }));
    }

    for t in 0..writer_threads {
        let db = Arc::clone(&db);
        let seed = salt ^ ((worker_index as u64) << 32) ^ ((t as u64) << 8) ^ 0x5A5A_5A5A;
        handles.push(thread::spawn(move || -> Result<ThreadMetrics> {
            let mut rng = Lcg::new(seed);
            let mut lat = LatencySampler::new(LATENCY_SAMPLE_CAP, rng.next_u64());
            let mut m = ThreadMetrics::default();
            let write_buf = prepare_data(chunk_size, worker_index.wrapping_add(t));
            let mut write_seq = 0_u64;
            let write_base = preload_chunks
                .saturating_add((worker_index as u64) << 40)
                .saturating_add((t as u64) << 24)
                .saturating_add(1);

            while Instant::now() < deadline {
                let key_idx = if write_keyspace_chunks == 0 {
                    write_base.saturating_add(write_seq)
                } else {
                    let slot = write_seq % write_keyspace_chunks;
                    write_base.saturating_add(slot)
                };
                let cs = checksum_for_key(salt, key_idx, method);
                let op_start = Instant::now();
                db.add_chunk(&cs, write_buf.clone())?;
                let op_ns = op_start.elapsed().as_nanos() as u64;
                lat.observe(op_ns);

                write_seq = write_seq.saturating_add(1);
                m.ops_total = m.ops_total.saturating_add(1);
                m.write_ops = m.write_ops.saturating_add(1);
                m.bytes_written = m.bytes_written.saturating_add(chunk_size as u64);
            }

            m.latency_seen = lat.seen;
            m.latency_samples = lat.samples;
            Ok(m)
        }));
    }

    let mut out = WorkerMetrics {
        worker_index,
        process_count,
        elapsed_ns: 0,
        ops_total: 0,
        read_ops: 0,
        write_ops: 0,
        read_misses: 0,
        bytes_read: 0,
        bytes_written: 0,
        latency_seen: 0,
        latency_samples: Vec::new(),
    };

    for handle in handles {
        let partial = handle
            .join()
            .map_err(|_| anyhow::anyhow!("mixed concurrent worker thread panicked"))??;
        out.ops_total = out.ops_total.saturating_add(partial.ops_total);
        out.read_ops = out.read_ops.saturating_add(partial.read_ops);
        out.write_ops = out.write_ops.saturating_add(partial.write_ops);
        out.read_misses = out.read_misses.saturating_add(partial.read_misses);
        out.bytes_read = out.bytes_read.saturating_add(partial.bytes_read);
        out.bytes_written = out.bytes_written.saturating_add(partial.bytes_written);
        out.latency_seen = out.latency_seen.saturating_add(partial.latency_seen);
        out.latency_samples
            .extend_from_slice(&partial.latency_samples);
    }
    out.elapsed_ns = start.elapsed().as_nanos();
    Ok(out)
}

fn parse_worker_metrics(stdout: &[u8]) -> Result<WorkerMetrics> {
    let s = String::from_utf8_lossy(stdout);
    let line = s
        .lines()
        .find(|l| l.starts_with(WORKER_PREFIX))
        .context("worker output missing metrics line")?;
    let payload = &line[WORKER_PREFIX.len()..];
    let v: Value = serde_json::from_str(payload).context("invalid worker metrics JSON")?;
    let samples = v["latency_samples_ns"]
        .as_array()
        .context("missing latency_samples_ns")?
        .iter()
        .map(|x| x.as_u64().context("invalid latency sample value"))
        .collect::<Result<Vec<_>>>()?;
    Ok(WorkerMetrics {
        worker_index: v["worker_index"].as_u64().unwrap_or(0) as u32,
        process_count: v["process_count"].as_u64().unwrap_or(1) as u32,
        elapsed_ns: v["elapsed_ns"].as_u64().unwrap_or(0) as u128,
        ops_total: v["ops_total"].as_u64().unwrap_or(0),
        read_ops: v["read_ops"].as_u64().unwrap_or(0),
        write_ops: v["write_ops"].as_u64().unwrap_or(0),
        read_misses: v["read_misses"].as_u64().unwrap_or(0),
        bytes_read: v["bytes_read"].as_u64().unwrap_or(0),
        bytes_written: v["bytes_written"].as_u64().unwrap_or(0),
        latency_seen: v["latency_seen"].as_u64().unwrap_or(0),
        latency_samples: samples,
    })
}

fn worker_metrics_to_wire(m: &WorkerMetrics) -> Value {
    json!({
        "worker_index": m.worker_index,
        "process_count": m.process_count,
        "elapsed_ns": m.elapsed_ns as u64,
        "ops_total": m.ops_total,
        "read_ops": m.read_ops,
        "write_ops": m.write_ops,
        "read_misses": m.read_misses,
        "bytes_read": m.bytes_read,
        "bytes_written": m.bytes_written,
        "latency_seen": m.latency_seen,
        "latency_samples_ns": m.latency_samples,
    })
}

fn aggregate(scenario: Scenario, opts: &Opts, workers: &[WorkerMetrics], salt: u64) -> Value {
    let ops_total: u64 = workers.iter().map(|w| w.ops_total).sum();
    let read_ops: u64 = workers.iter().map(|w| w.read_ops).sum();
    let write_ops: u64 = workers.iter().map(|w| w.write_ops).sum();
    let read_misses: u64 = workers.iter().map(|w| w.read_misses).sum();
    let bytes_read: u64 = workers.iter().map(|w| w.bytes_read).sum();
    let bytes_written: u64 = workers.iter().map(|w| w.bytes_written).sum();
    let elapsed_ns: u128 = workers.iter().map(|w| w.elapsed_ns).max().unwrap_or(0);
    let elapsed_secs = ns_to_secs(elapsed_ns);

    let mut all_samples = Vec::new();
    let mut seen_sum: u64 = 0;
    for w in workers {
        seen_sum = seen_sum.saturating_add(w.latency_seen);
        all_samples.extend_from_slice(&w.latency_samples);
    }
    all_samples.sort_unstable();

    json!({
        "bench": {
            "scenario": format!("{scenario:?}").to_lowercase(),
            "db_path": opts.db_path,
            "chunk_size": opts.chunk_size,
            "duration_secs": opts.duration_secs,
            "preload_chunks": opts.preload_chunks,
            "processes": workers.len(),
            "write_percent": opts.write_percent,
            "write_keyspace_chunks": opts.write_keyspace_chunks,
            "reset_db": opts.reset_db,
            "read_copy": opts.read_copy,
            "mixed_concurrent": opts.mixed_concurrent,
            "mixed_reader_threads": opts.mixed_reader_threads,
            "mixed_writer_threads": opts.mixed_writer_threads,
            "hash_method": format!("{:?}", opts.hash_method).to_lowercase(),
            "run_salt": salt,
        },
        "aggregate": {
            "elapsed_ns": elapsed_ns,
            "elapsed_secs": elapsed_secs,
            "ops_total": ops_total,
            "read_ops": read_ops,
            "write_ops": write_ops,
            "read_misses": read_misses,
            "bytes_read": bytes_read,
            "bytes_written": bytes_written,
            "ops_per_sec": rate(ops_total, elapsed_secs),
            "read_ops_per_sec": rate(read_ops, elapsed_secs),
            "write_ops_per_sec": rate(write_ops, elapsed_secs),
            "mb_read_per_sec": bytes_rate_mb(bytes_read, elapsed_secs),
            "mb_write_per_sec": bytes_rate_mb(bytes_written, elapsed_secs),
            "latency_seen": seen_sum,
            "latency_sample_count": all_samples.len(),
            "latency_ns_p50": percentile(&all_samples, 0.50),
            "latency_ns_p95": percentile(&all_samples, 0.95),
            "latency_ns_p99": percentile(&all_samples, 0.99),
        },
        "workers": workers.iter().map(WorkerMetrics::to_json).collect::<Vec<_>>(),
    })
}

fn spawn_workers(opts: &Opts, salt: u64) -> Result<Vec<WorkerMetrics>> {
    let exe = std::env::current_exe().context("failed to locate current executable")?;
    let mut children = Vec::new();

    for worker_idx in 0..opts.processes {
        let mut cmd = Command::new(&exe);
        cmd.arg("--db-path").arg(&opts.db_path);
        cmd.arg("--scenario")
            .arg(format!("{:?}", opts.scenario).to_lowercase());
        cmd.arg("--chunk-size").arg(opts.chunk_size.to_string());
        cmd.arg("--duration-secs")
            .arg(opts.duration_secs.to_string());
        cmd.arg("--preload-chunks")
            .arg(opts.preload_chunks.to_string());
        cmd.arg("--processes").arg("1");
        cmd.arg("--write-percent")
            .arg(opts.write_percent.to_string());
        cmd.arg("--write-keyspace-chunks")
            .arg(opts.write_keyspace_chunks.to_string());
        if opts.reset_db {
            cmd.arg("--reset-db");
        }
        cmd.arg("--hash-method")
            .arg(format!("{:?}", opts.hash_method).to_lowercase());
        if opts.read_copy {
            cmd.arg("--read-copy");
        }
        if opts.mixed_concurrent {
            cmd.arg("--mixed-concurrent");
        }
        cmd.arg("--mixed-reader-threads")
            .arg(opts.mixed_reader_threads.to_string());
        cmd.arg("--mixed-writer-threads")
            .arg(opts.mixed_writer_threads.to_string());
        cmd.arg("--worker-index").arg(worker_idx.to_string());
        cmd.arg("--run-salt").arg(salt.to_string());
        cmd.arg("--json-only");
        cmd.arg("--skip-preload");
        cmd.stdout(Stdio::piped());
        cmd.stderr(Stdio::inherit());
        children.push(cmd.spawn().context("failed to spawn worker process")?);
    }

    let mut reports = Vec::new();
    for child in children {
        let out = child
            .wait_with_output()
            .context("failed waiting for worker output")?;
        if !out.status.success() {
            anyhow::bail!("worker exited with status {}", out.status);
        }
        let report = parse_worker_metrics(&out.stdout)?;
        reports.push(report);
    }
    Ok(reports)
}

fn main() -> Result<()> {
    let opts = Opts::parse();
    if opts.processes == 0 {
        anyhow::bail!("processes must be >= 1");
    }
    if opts.chunk_size == 0 {
        anyhow::bail!("chunk_size must be > 0");
    }
    if opts.mixed_concurrent && opts.scenario != Scenario::Mixed {
        anyhow::bail!("mixed_concurrent is only valid when scenario is mixed");
    }
    if opts.mixed_concurrent && (opts.mixed_reader_threads == 0 || opts.mixed_writer_threads == 0) {
        anyhow::bail!("mixed concurrent mode requires reader/writer threads > 0");
    }

    let salt = opts.run_salt.unwrap_or_else(|| {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_nanos() as u64)
            .unwrap_or(1)
    });

    let worker_index = opts.worker_index.unwrap_or(0);
    if opts.worker_index.is_some() {
        fs::create_dir_all(&opts.db_path)?;
        let db = Arc::new(ChunkDB::new(PathBuf::from(&opts.db_path))?);
        if !opts.skip_preload {
            preload_for_reads(db.as_ref(), &opts, salt)?;
        }
        let metrics = run_worker(Arc::clone(&db), &opts, worker_index, 1, salt)?;
        let wire = worker_metrics_to_wire(&metrics);
        println!("{WORKER_PREFIX}{}", serde_json::to_string(&wire)?);
        return Ok(());
    }

    if opts.reset_db {
        let _ = fs::remove_dir_all(&opts.db_path);
    }
    fs::create_dir_all(&opts.db_path)?;
    let db = Arc::new(ChunkDB::new(PathBuf::from(&opts.db_path))?);
    if !opts.skip_preload {
        preload_for_reads(db.as_ref(), &opts, salt)?;
    }

    let reports = if opts.processes == 1 {
        vec![run_worker(Arc::clone(&db), &opts, 0, 1, salt)?]
    } else {
        spawn_workers(&opts, salt)?
    };

    let result = aggregate(opts.scenario, &opts, &reports, salt);
    if opts.json_only {
        println!("{}", serde_json::to_string(&result)?);
    } else {
        println!("{}", serde_json::to_string_pretty(&result)?);
    }
    Ok(())
}
