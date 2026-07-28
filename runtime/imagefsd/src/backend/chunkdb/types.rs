use bytemuck::{Pod, Zeroable};
use heed::{BoxedError, BytesDecode, BytesEncode};
use sha2::Digest;
use std::borrow::Cow;
use std::cmp::Ordering;
use std::fmt::{Display, Formatter};
use std::io::{self, ErrorKind};

#[derive(Debug, Copy, Clone, Eq, PartialEq, Ord, PartialOrd)]
pub enum CheckSumMethod {
    Sha256 = 0,
    Blake3 = 1,
    Unknown = 2,
}

impl From<u8> for CheckSumMethod {
    fn from(value: u8) -> Self {
        match value {
            0 => Self::Sha256,
            1 => Self::Blake3,
            _ => Self::Unknown,
        }
    }
}

impl From<CheckSumMethod> for u8 {
    fn from(value: CheckSumMethod) -> Self {
        value as u8
    }
}

#[derive(Debug, Copy, Clone, Eq, PartialEq)]
pub struct CheckSum {
    pub(crate) raw: [u8; 32],
    pub(crate) method: CheckSumMethod,
}

impl Display for CheckSum {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(f, "{:?}:{}", self.method, hex::encode(self.raw))
    }
}

impl Ord for CheckSum {
    fn cmp(&self, other: &Self) -> Ordering {
        if self.method != other.method {
            self.method.cmp(&other.method)
        } else {
            self.raw.cmp(&other.raw)
        }
    }
}

impl PartialOrd for CheckSum {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl CheckSum {
    pub fn empty() -> Self {
        Self {
            raw: [0_u8; 32],
            method: CheckSumMethod::Unknown,
        }
    }

    pub fn new(raw: &[u8], method: CheckSumMethod) -> io::Result<Self> {
        let mut cs = Self {
            raw: [0_u8; 32],
            method,
        };
        if cs.raw.len() != raw.len() {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                "Invalid checksum data len",
            ));
        }
        cs.raw.copy_from_slice(raw);
        Ok(cs)
    }

    pub fn from_data(data: &[u8], method: CheckSumMethod) -> Self {
        let raw: [u8; 32] = match method {
            CheckSumMethod::Sha256 => sha2::Sha256::new().chain_update(data).finalize().into(),
            CheckSumMethod::Blake3 => blake3::hash(data).into(),
            _ => blake3::hash(data).into(),
        };
        Self::new(&raw, method).unwrap()
    }
}

#[derive(Debug, Copy, Clone, Eq, PartialEq, Hash, Pod, Zeroable)]
#[repr(C)]
pub(crate) struct CheckSumOnDisk {
    raw: [u8; 32],
    method: u8,
    reserved: [u8; 7],
}

impl CheckSumOnDisk {
    pub(crate) fn is_valid(&self) -> bool {
        self.method < 2
    }
}

impl From<CheckSum> for CheckSumOnDisk {
    fn from(value: CheckSum) -> Self {
        let method = value.method.into();
        Self {
            raw: value.raw,
            method,
            reserved: [0_u8; 7],
        }
    }
}

impl From<CheckSumOnDisk> for CheckSum {
    fn from(value: CheckSumOnDisk) -> Self {
        let method = value.method.into();
        Self {
            raw: value.raw,
            method,
        }
    }
}

impl<'a> BytesEncode<'a> for CheckSumOnDisk {
    type EItem = CheckSumOnDisk;

    fn bytes_encode(item: &'a Self::EItem) -> Result<Cow<'a, [u8]>, BoxedError> {
        Ok(Cow::Borrowed(bytemuck::bytes_of(item)))
    }
}

impl<'a> BytesDecode<'a> for CheckSumOnDisk {
    type DItem = &'a CheckSumOnDisk;

    fn bytes_decode(bytes: &'a [u8]) -> Result<Self::DItem, BoxedError> {
        Ok(bytemuck::from_bytes(bytes))
    }
}

impl Ord for CheckSumOnDisk {
    fn cmp(&self, other: &Self) -> Ordering {
        if self.method != other.method {
            self.method.cmp(&other.method)
        } else {
            self.raw.cmp(&other.raw)
        }
    }
}

impl PartialOrd for CheckSumOnDisk {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

#[derive(Debug, Copy, Clone, Eq, PartialEq)]
pub(super) struct AccessTime {
    pub(super) secs: u64,
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct GcDeleteResult {
    pub removed: usize,
    pub checksums: Vec<CheckSum>,
}

impl<'a> BytesEncode<'a> for AccessTime {
    type EItem = AccessTime;

    fn bytes_encode(item: &'a Self::EItem) -> Result<Cow<'a, [u8]>, BoxedError> {
        Ok(Cow::Owned(item.secs.to_be_bytes().to_vec()))
    }
}

impl<'a> BytesDecode<'a> for AccessTime {
    type DItem = AccessTime;

    fn bytes_decode(bytes: &'a [u8]) -> Result<Self::DItem, BoxedError> {
        if bytes.len() != 8 {
            return Err("Invalid access time length".into());
        }
        Ok(AccessTime {
            secs: u64::from_be_bytes(bytes.try_into()?),
        })
    }
}

#[derive(Debug, Copy, Clone, Eq, PartialEq)]
pub(super) struct AccessKey {
    pub(super) last_access: u64,
    pub(super) cs: CheckSumOnDisk,
}

impl AccessKey {
    pub(super) fn new(last_access: u64, cs: CheckSumOnDisk) -> Self {
        Self { last_access, cs }
    }
}

impl Ord for AccessKey {
    fn cmp(&self, other: &Self) -> Ordering {
        match self.last_access.cmp(&other.last_access) {
            Ordering::Equal => self.cs.cmp(&other.cs),
            other => other,
        }
    }
}

impl PartialOrd for AccessKey {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl<'a> BytesEncode<'a> for AccessKey {
    type EItem = AccessKey;

    fn bytes_encode(item: &'a Self::EItem) -> Result<Cow<'a, [u8]>, BoxedError> {
        let mut key = Vec::with_capacity(8 + std::mem::size_of::<CheckSumOnDisk>());
        key.extend_from_slice(&item.last_access.to_be_bytes());
        key.extend_from_slice(bytemuck::bytes_of(&item.cs));
        Ok(Cow::Owned(key))
    }
}

impl<'a> BytesDecode<'a> for AccessKey {
    type DItem = AccessKey;

    fn bytes_decode(bytes: &'a [u8]) -> Result<Self::DItem, BoxedError> {
        let expected = 8 + std::mem::size_of::<CheckSumOnDisk>();
        if bytes.len() != expected {
            return Err("Invalid access key length".into());
        }
        let last_access = u64::from_be_bytes(bytes[0..8].try_into()?);
        let cs = *bytemuck::from_bytes(&bytes[8..expected]);
        Ok(AccessKey { last_access, cs })
    }
}
