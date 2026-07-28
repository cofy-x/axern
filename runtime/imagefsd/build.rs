use std::path::Path;

fn main() {
    // Try reading the git commit hash without invoking git.
    // This works in normal checkouts and CI environments where
    // the git binary may not be available.
    let hash = read_git_head().unwrap_or_else(|| "unknown".to_string());
    println!("cargo:rustc-env=GIT_COMMIT_HASH={}", hash.trim());
    println!("cargo:rerun-if-changed=.git/HEAD");
    println!("cargo:rerun-if-changed=.git/refs/heads/");
}

/// Resolve HEAD → short commit hash by reading .git files directly.
fn read_git_head() -> Option<String> {
    let head = std::fs::read_to_string(".git/HEAD").ok()?;
    let head = head.trim();
    let full_hash = if let Some(ref_path) = head.strip_prefix("ref: ") {
        // HEAD points to a branch ref, e.g. "ref: refs/heads/main"
        let ref_file = Path::new(".git").join(ref_path);
        // Try unpacked ref first, then fall back to packed-refs
        std::fs::read_to_string(&ref_file)
            .ok()
            .or_else(|| resolve_packed_ref(ref_path))
    } else {
        // Detached HEAD — value is the commit hash itself
        Some(head.to_string())
    }?;
    let full_hash = full_hash.trim();
    if full_hash.len() >= 7 {
        Some(full_hash[..7].to_string())
    } else {
        None
    }
}

/// Look up a ref in .git/packed-refs (used after `git gc` or in some CI setups).
fn resolve_packed_ref(ref_path: &str) -> Option<String> {
    let packed = std::fs::read_to_string(".git/packed-refs").ok()?;
    for line in packed.lines() {
        if line.starts_with('#') {
            continue;
        }
        // Format: "<hash> <ref>"
        if let Some((hash, r)) = line.split_once(' ') {
            if r == ref_path {
                return Some(hash.to_string());
            }
        }
    }
    None
}
