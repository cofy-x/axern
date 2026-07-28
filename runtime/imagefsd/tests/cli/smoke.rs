use std::process::Command;

#[test]
fn cli_help_smoke() {
    let output = Command::new(env!("CARGO_BIN_EXE_imagefsd"))
        .arg("--help")
        .output()
        .unwrap();

    assert!(output.status.success());
    assert!(String::from_utf8_lossy(&output.stdout).contains("dedup, on_demand, readonly fs"));
}

#[test]
fn cli_subcommand_help_smoke() {
    let output = Command::new(env!("CARGO_BIN_EXE_imagefsd"))
        .args(["serve-chunk", "--help"])
        .output()
        .unwrap();

    assert!(output.status.success());
    assert!(String::from_utf8_lossy(&output.stdout).contains("--listen-port"));
}

#[test]
fn gc_chunk_help_mentions_chunk_server_sock() {
    let output = Command::new(env!("CARGO_BIN_EXE_imagefsd"))
        .args(["gc-chunk", "--help"])
        .output()
        .unwrap();

    assert!(output.status.success());
    assert!(String::from_utf8_lossy(&output.stdout).contains("--chunk-server-sock"));
}

#[test]
fn cli_version_includes_git_hash() {
    let output = Command::new(env!("CARGO_BIN_EXE_imagefsd"))
        .arg("--version")
        .output()
        .unwrap();

    assert!(output.status.success());
    let version = String::from_utf8_lossy(&output.stdout);
    // Version format: "imagefsd 0.1 (abc1234)"
    assert!(
        version.contains('(') && version.contains(')'),
        "version should contain git hash in parens: {version}"
    );
}

#[test]
fn cli_stats_chunk_outputs_json_to_stdout() {
    let temp = tempfile::TempDir::new().unwrap();
    // stats-chunk on an empty (non-initialized) directory may fail,
    // but on an initialized one it should output JSON to stdout.
    // Initialize LMDB by opening ChunkDB.
    drop(imagefsd::backend::chunkdb::ChunkDB::new(temp.path()).unwrap());

    let output = Command::new(env!("CARGO_BIN_EXE_imagefsd"))
        .args([
            "stats-chunk",
            "--chunk-db-dir",
            temp.path().to_str().unwrap(),
        ])
        .output()
        .unwrap();

    assert!(output.status.success());
    let stdout = String::from_utf8_lossy(&output.stdout);
    let parsed: serde_json::Value =
        serde_json::from_str(&stdout).expect("stdout should be valid JSON");
    assert!(parsed["storage"]["total_size_bytes"].as_u64().is_some());
    assert!(parsed["readers"]["max"].as_u64().is_some());
}
