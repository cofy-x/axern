use anyhow::{Context, Result};
use clap::{Parser, ValueEnum};
use imagefsd::backend::chunkdb::{CheckSum, CheckSumMethod, ChunkDB};
use imagefsd::backend::peer::{ChunkServer, PeerClient, PeerRuntime, StaticPeers};
use imagefsd::backend::CHUNK_SIZE;
use serde_json::{json, Value};
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};
use tempfile::TempDir;

const DEFAULT_CHUNK_SIZE: usize = 4 * 1024 * 1024;
const DEFAULT_UNIQUE_CHUNKS: u64 = 64;
const DEFAULT_WORKERS: usize = 4;
const DEFAULT_DURATION_SECS: u64 = 10;
const DEFAULT_WARMUP_SECS: u64 = 2;
const LATENCY_SAMPLE_CAP: usize = 50_000;

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
#[command(name = "peer_bench")]
#[command(about = "Peer-to-peer local TCP chunk transfer benchmark")]
struct Opts {
    #[arg(long)]
    db_path: String,

    #[arg(long, default_value_t = DEFAULT_CHUNK_SIZE)]
    chunk_size: usize,

    #[arg(long, default_value_t = DEFAULT_UNIQUE_CHUNKS)]
    unique_chunks: u64,

    #[arg(long, default_value_t = DEFAULT_WORKERS)]
    workers: usize,

    #[arg(long, default_value_t = DEFAULT_WARMUP_SECS)]
    warmup_secs: u64,

    #[arg(long, default_value_t = DEFAULT_DURATION_SECS)]
    duration_secs: u64,

    #[arg(long, default_value_t = false)]
    reset_db: bool,

    #[arg(long, value_enum, default_value_t = HashMethod::Blake3)]
    hash_method: HashMethod,

    #[arg(long, default_value_t = false)]
    verify_payload: bool,

    #[arg(long, default_value_t = false)]
    json_only: bool,
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

#[derive(Debug, Default)]
struct WorkerMetrics {
    worker_index: usize,
    elapsed_ns: u128,
    ops_total: u64,
    bytes_read: u64,
    misses: u64,
    verify_failures: u64,
    latency_seen: u64,
    latency_samples: Vec<u64>,
}

impl WorkerMetrics {
    fn to_json(&self) -> Value {
        let mut samples = self.latency_samples.clone();
        samples.sort_unstable();
        let elapsed_secs = ns_to_secs(self.elapsed_ns);
        json!({
            "worker_index": self.worker_index,
            "elapsed_ns": self.elapsed_ns,
            "elapsed_secs": elapsed_secs,
            "ops_total": self.ops_total,
            "bytes_read": self.bytes_read,
            "misses": self.misses,
            "verify_failures": self.verify_failures,
            "ops_per_sec": rate(self.ops_total, elapsed_secs),
            "mb_read_per_sec": bytes_rate_mb(self.bytes_read, elapsed_secs),
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

fn prepare_data(chunk_size: usize, chunk_index: u64) -> Vec<u8> {
    let mut buf = vec![0_u8; chunk_size];
    let base = (chunk_index % 251) as u8;
    for (i, v) in buf.iter_mut().enumerate() {
        *v = base.wrapping_add((i % 251) as u8);
    }
    buf
}

fn preload_chunks(
    db: &ChunkDB,
    unique_chunks: u64,
    chunk_size: usize,
    method: CheckSumMethod,
) -> Result<Vec<CheckSum>> {
    let mut checksums = Vec::with_capacity(unique_chunks as usize);
    for idx in 0..unique_chunks {
        let data = prepare_data(chunk_size, idx);
        let checksum = CheckSum::from_data(&data, method);
        db.add_chunk(&checksum, data)?;
        checksums.push(checksum);
    }
    Ok(checksums)
}

fn wait_for_server_ready(addr: std::net::SocketAddr, unix_sock: &Path) -> Result<()> {
    let deadline = Instant::now() + Duration::from_secs(5);
    while Instant::now() < deadline {
        if unix_sock.exists() && std::net::TcpStream::connect(addr).is_ok() {
            return Ok(());
        }
        thread::sleep(Duration::from_millis(20));
    }
    anyhow::bail!("chunk server did not become ready in time");
}

async fn run_worker(
    client: Arc<PeerClient>,
    checksums: Arc<Vec<CheckSum>>,
    opts: Arc<Opts>,
    worker_index: usize,
) -> WorkerMetrics {
    let mut cursor = worker_index as u64;
    let mut lat = LatencySampler::new(LATENCY_SAMPLE_CAP, (worker_index as u64) + 1);

    let warmup_deadline = Instant::now() + Duration::from_secs(opts.warmup_secs);
    while Instant::now() < warmup_deadline {
        let checksum = &checksums[(cursor % checksums.len() as u64) as usize];
        let _ = client.fetch_chunk(checksum).await;
        cursor = cursor.saturating_add(1);
    }

    let mut out = WorkerMetrics {
        worker_index,
        ..Default::default()
    };
    let start = Instant::now();
    let deadline = start + Duration::from_secs(opts.duration_secs);
    while Instant::now() < deadline {
        let chunk_idx = cursor % checksums.len() as u64;
        let checksum = &checksums[chunk_idx as usize];
        let op_start = Instant::now();
        match client.fetch_chunk(checksum).await {
            Some(data) => {
                if data.len() == opts.chunk_size {
                    out.bytes_read = out.bytes_read.saturating_add(data.len() as u64);
                } else {
                    out.verify_failures = out.verify_failures.saturating_add(1);
                }
                if opts.verify_payload {
                    let expected = prepare_data(opts.chunk_size, chunk_idx);
                    if data != expected {
                        out.verify_failures = out.verify_failures.saturating_add(1);
                    }
                }
            }
            None => {
                out.misses = out.misses.saturating_add(1);
            }
        }
        lat.observe(op_start.elapsed().as_nanos() as u64);
        out.ops_total = out.ops_total.saturating_add(1);
        cursor = cursor.saturating_add(1);
    }
    out.elapsed_ns = start.elapsed().as_nanos();
    out.latency_seen = lat.seen;
    out.latency_samples = lat.samples;
    out
}

fn aggregate(opts: &Opts, workers: &[WorkerMetrics]) -> Value {
    let elapsed_ns = workers.iter().map(|w| w.elapsed_ns).max().unwrap_or(0);
    let elapsed_secs = ns_to_secs(elapsed_ns);
    let ops_total: u64 = workers.iter().map(|w| w.ops_total).sum();
    let bytes_read: u64 = workers.iter().map(|w| w.bytes_read).sum();
    let misses: u64 = workers.iter().map(|w| w.misses).sum();
    let verify_failures: u64 = workers.iter().map(|w| w.verify_failures).sum();
    let mut seen_sum = 0_u64;
    let mut all_samples = Vec::new();
    for worker in workers {
        seen_sum = seen_sum.saturating_add(worker.latency_seen);
        all_samples.extend_from_slice(&worker.latency_samples);
    }
    all_samples.sort_unstable();

    json!({
        "bench": {
            "mode": "peer_local_tcp",
            "db_path": opts.db_path,
            "chunk_size": opts.chunk_size,
            "unique_chunks": opts.unique_chunks,
            "workers": opts.workers,
            "warmup_secs": opts.warmup_secs,
            "duration_secs": opts.duration_secs,
            "reset_db": opts.reset_db,
            "hash_method": format!("{:?}", opts.hash_method).to_lowercase(),
            "verify_payload": opts.verify_payload,
        },
        "aggregate": {
            "elapsed_ns": elapsed_ns,
            "elapsed_secs": elapsed_secs,
            "ops_total": ops_total,
            "bytes_read": bytes_read,
            "misses": misses,
            "verify_failures": verify_failures,
            "ops_per_sec": rate(ops_total, elapsed_secs),
            "mb_read_per_sec": bytes_rate_mb(bytes_read, elapsed_secs),
            "latency_seen": seen_sum,
            "latency_sample_count": all_samples.len(),
            "latency_ns_p50": percentile(&all_samples, 0.50),
            "latency_ns_p95": percentile(&all_samples, 0.95),
            "latency_ns_p99": percentile(&all_samples, 0.99),
        },
        "workers": workers.iter().map(WorkerMetrics::to_json).collect::<Vec<_>>(),
    })
}

fn main() -> Result<()> {
    let opts = Opts::parse();
    if opts.chunk_size == 0 {
        anyhow::bail!("chunk_size must be > 0");
    }
    if opts.chunk_size > CHUNK_SIZE {
        anyhow::bail!("chunk_size must be <= {} bytes", CHUNK_SIZE);
    }
    if opts.unique_chunks == 0 {
        anyhow::bail!("unique_chunks must be > 0");
    }
    if opts.workers == 0 {
        anyhow::bail!("workers must be > 0");
    }

    if opts.reset_db {
        let _ = fs::remove_dir_all(&opts.db_path);
    }
    fs::create_dir_all(&opts.db_path)
        .with_context(|| format!("failed to create db directory: {}", opts.db_path))?;

    let method: CheckSumMethod = opts.hash_method.into();
    let server_runtime = PeerRuntime::new()?;
    let client_runtime = PeerRuntime::new()?;
    let db = Arc::new(ChunkDB::new(PathBuf::from(&opts.db_path))?);
    let checksums = Arc::new(preload_chunks(
        db.as_ref(),
        opts.unique_chunks,
        opts.chunk_size,
        method,
    )?);

    let socket_root = TempDir::new().context("failed to create temp socket directory")?;
    let socket_path = socket_root.path().join("chunkserver.sock");
    let server = ChunkServer::new(
        server_runtime.clone(),
        Arc::clone(&db),
        "127.0.0.1:0".parse().unwrap(),
        &socket_path,
        None,
    )?;
    let addr = server.local_addr()?;
    let shutdown = server.shutdown_handle();
    let server_handle = thread::spawn(move || {
        if let Err(err) = server.run() {
            eprintln!("chunk server failed: {err:#}");
        }
    });

    wait_for_server_ready(addr, &socket_path)?;

    let discovery = Arc::new(StaticPeers::new(vec![addr]));
    let client = Arc::new(PeerClient::new(client_runtime.clone(), discovery));

    if opts.verify_payload {
        let data = client_runtime
            .block_on(client.fetch_chunk(&checksums[0]))
            .context("verification fetch returned miss")?;
        let expected = prepare_data(opts.chunk_size, 0);
        if data != expected {
            anyhow::bail!("verification fetch did not match expected payload");
        }
    }

    let shared_opts = Arc::new(opts.clone());
    let workers = client_runtime.block_on(async {
        let mut tasks = Vec::with_capacity(shared_opts.workers);
        for worker_index in 0..shared_opts.workers {
            let client = Arc::clone(&client);
            let checksums = Arc::clone(&checksums);
            let opts = Arc::clone(&shared_opts);
            tasks.push(
                client_runtime
                    .spawn(async move { run_worker(client, checksums, opts, worker_index).await }),
            );
        }

        let mut reports = Vec::with_capacity(tasks.len());
        for task in tasks {
            reports.push(task.await.context("worker task panicked")?);
        }
        Ok::<Vec<WorkerMetrics>, anyhow::Error>(reports)
    })?;

    shutdown.shutdown();
    server_handle
        .join()
        .map_err(|_| anyhow::anyhow!("chunk server thread panicked"))?;

    let result = aggregate(&opts, &workers);
    if opts.json_only {
        println!("{}", serde_json::to_string(&result)?);
    } else {
        println!("{}", serde_json::to_string_pretty(&result)?);
    }
    Ok(())
}
