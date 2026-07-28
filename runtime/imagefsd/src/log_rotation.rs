use std::ffi::OsString;
use std::fs::{self, File, OpenOptions};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

const MAX_LOG_SIZE: u64 = 10 * 1024 * 1024; // 10MB
const MAX_LOG_FILES: usize = 3;

/// A writer that automatically rotates log files when they exceed MAX_LOG_SIZE.
/// Keeps the last MAX_LOG_FILES files (e.g., app.log, app.log.1, app.log.2).
pub struct RotatingFileWriter {
    inner: Arc<Mutex<RotatingFileWriterInner>>,
}

struct RotatingFileWriterInner {
    base_path: PathBuf,
    current_file: File,
    current_size: u64,
}

impl RotatingFileWriter {
    /// Create a new rotating file writer.
    /// If the log file already exists, it will append to it and check for rotation.
    pub fn new<P: AsRef<Path>>(path: P) -> io::Result<Self> {
        let base_path = path.as_ref().to_path_buf();
        let (current_file, current_size) = Self::open_or_create_file(&base_path)?;

        let inner = RotatingFileWriterInner {
            base_path,
            current_file,
            current_size,
        };

        Ok(Self {
            inner: Arc::new(Mutex::new(inner)),
        })
    }

    fn open_or_create_file(path: &Path) -> io::Result<(File, u64)> {
        let file = OpenOptions::new().create(true).append(true).open(path)?;

        let metadata = file.metadata()?;
        let size = metadata.len();

        Ok((file, size))
    }

    fn rotate_files(base_path: &Path) -> io::Result<()> {
        // Delete the oldest log file if it exists (e.g., app.log.2)
        let oldest = Self::rotated_path(base_path, MAX_LOG_FILES - 1);
        if oldest.exists() {
            fs::remove_file(&oldest)?;
        }

        // Rotate existing files (e.g., app.log.1 -> app.log.2, app.log -> app.log.1)
        for i in (1..MAX_LOG_FILES - 1).rev() {
            let src = Self::rotated_path(base_path, i);
            let dst = Self::rotated_path(base_path, i + 1);
            if src.exists() {
                fs::rename(&src, &dst)?;
            }
        }

        // Rename current log file to .1
        let current = base_path;
        let first_rotation = Self::rotated_path(base_path, 1);
        if current.exists() {
            fs::rename(current, first_rotation)?;
        }

        Ok(())
    }

    fn rotated_path(base_path: &Path, index: usize) -> PathBuf {
        if let Some(file_name) = base_path.file_name() {
            let mut rotated_name = OsString::from(file_name);
            rotated_name.push(format!(".{}", index));
            base_path.with_file_name(rotated_name)
        } else {
            // Fallback for unusual paths; keep previous behavior.
            let mut path = base_path.to_path_buf();
            path.set_extension(format!("{}", index));
            path
        }
    }
}

impl Write for RotatingFileWriter {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        let mut inner = self.inner.lock().unwrap();

        // Check if rotation is needed
        if inner.current_size + buf.len() as u64 > MAX_LOG_SIZE {
            // Flush current file before rotating
            inner.current_file.flush()?;

            // Perform rotation
            Self::rotate_files(&inner.base_path)?;

            // Open new file
            let (new_file, new_size) = Self::open_or_create_file(&inner.base_path)?;
            inner.current_file = new_file;
            inner.current_size = new_size;
        }

        // Write to current file
        let written = inner.current_file.write(buf)?;
        inner.current_size += written as u64;

        Ok(written)
    }

    fn flush(&mut self) -> io::Result<()> {
        let mut inner = self.inner.lock().unwrap();
        inner.current_file.flush()
    }
}

impl Clone for RotatingFileWriter {
    fn clone(&self) -> Self {
        Self {
            inner: Arc::clone(&self.inner),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use tempfile::TempDir;

    #[test]
    fn test_rotating_writer_basic() {
        let temp_dir = TempDir::new().unwrap();
        let log_path = temp_dir.path().join("test.log");

        let mut writer = RotatingFileWriter::new(&log_path).unwrap();

        // Write some data
        let data = b"test log line\n";
        writer.write_all(data).unwrap();
        writer.flush().unwrap();

        // Verify file exists and contains data
        assert!(log_path.exists());
        let content = fs::read_to_string(&log_path).unwrap();
        assert!(content.contains("test log line"));
    }

    #[test]
    fn test_rotation_on_size_limit() {
        let temp_dir = TempDir::new().unwrap();
        let log_path = temp_dir.path().join("test.log");

        let mut writer = RotatingFileWriter::new(&log_path).unwrap();

        // Write data exceeding MAX_LOG_SIZE to trigger rotation
        let chunk = vec![b'A'; 1024 * 1024]; // 1MB chunk
        for _ in 0..11 {
            writer.write_all(&chunk).unwrap();
        }
        writer.flush().unwrap();

        // Check that rotation occurred
        let rotated_path = log_path.with_file_name("test.log.1");
        assert!(rotated_path.exists(), "Rotated file should exist");
        assert!(log_path.exists(), "Current log file should exist");

        // Verify current file is smaller than MAX_LOG_SIZE
        let metadata = fs::metadata(&log_path).unwrap();
        assert!(metadata.len() < MAX_LOG_SIZE);
    }

    #[test]
    fn test_multiple_rotations() {
        let temp_dir = TempDir::new().unwrap();
        let log_path = temp_dir.path().join("test.log");

        let mut writer = RotatingFileWriter::new(&log_path).unwrap();

        // Write enough data to trigger multiple rotations
        let chunk = vec![b'B'; 1024 * 1024]; // 1MB chunk
        for _ in 0..35 {
            writer.write_all(&chunk).unwrap();
        }
        writer.flush().unwrap();

        // Should have max MAX_LOG_FILES files
        assert!(log_path.exists());
        assert!(log_path.with_file_name("test.log.1").exists());
        assert!(log_path.with_file_name("test.log.2").exists());

        // The 4th file should not exist (we only keep 3)
        assert!(!log_path.with_file_name("test.log.3").exists());
    }
}
