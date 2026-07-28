use crate::backend::{Backend, BackendEx};
use crate::utils::new_std_io_error;
use nydus_api::BackendConfigV2;
use nydus_storage::backend::{BlobBackend, BlobReader};
use nydus_storage::factory::BlobFactory;
use std::fmt::{Debug, Formatter};
use std::fs::File;
use std::io::{self, ErrorKind};
use std::path::Path;
use std::sync::Arc;

pub struct BackendReader {
    name: String,
    size: u64,
    reader: Arc<dyn BlobReader>,
}

impl BackendReader {
    fn new(name: &str, reader: Arc<dyn BlobReader>, size: Option<u64>) -> io::Result<Self> {
        let size = match size {
            Some(size) => size,
            None => reader.blob_size().map_err(new_std_io_error)?,
        };
        Ok(Self {
            name: name.to_string(),
            size,
            reader,
        })
    }
}

impl Debug for BackendReader {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(f, "backend_reader:{}({})", self.name, self.size)
    }
}

impl Backend for BackendReader {
    fn size(&self) -> u64 {
        self.size
    }

    fn fetch(&self, off: usize, data: &mut [u8]) -> io::Result<usize> {
        self.reader.read(data, off as u64).map_err(new_std_io_error)
    }
}

impl BackendEx for BackendReader {
    fn invalidate_chunk(&self, _chunk_id: usize) -> io::Result<()> {
        Ok(())
    }
}

pub struct GeneralBackend {
    backend_cfg: BackendConfigV2,
    backend: Arc<dyn BlobBackend + Send + Sync>,
}

impl GeneralBackend {
    pub fn new<P: AsRef<Path>>(cfg_path: P) -> io::Result<Self> {
        let file = File::open(cfg_path)?;
        let cfg: BackendConfigV2 = serde_json::from_reader(file).map_err(new_std_io_error)?;
        if !cfg.validate() {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                "Invalid backend config",
            ));
        }
        let backend = BlobFactory::new_backend(&cfg, "imagefsd").map_err(new_std_io_error)?;
        Ok(Self {
            backend_cfg: cfg,
            backend,
        })
    }

    pub fn get_reader(&self, object: &str) -> io::Result<BackendReader> {
        let reader = self.backend.get_reader(object).map_err(new_std_io_error)?;
        BackendReader::new(object, reader, None)
    }

    pub fn get_reader_with_size(&self, object: &str, size: u64) -> io::Result<BackendReader> {
        if size == 0 {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                "backend object size must be greater than zero",
            ));
        }
        let reader = self.backend.get_reader(object).map_err(new_std_io_error)?;
        BackendReader::new(object, reader, Some(size))
    }

    pub fn backend_config(&self) -> &BackendConfigV2 {
        &self.backend_cfg
    }
}

impl Debug for GeneralBackend {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(f, "general_backend:{}", self.backend_cfg.backend_type)
    }
}
