use super::{MAX_CHUNK_PAYLOAD_SIZE, REQUEST_LEN, RESPONSE_HEADER_LEN};
use crate::backend::chunkdb::CheckSum;
#[cfg(test)]
use std::io::Read;
use std::io::{self, ErrorKind};
use tokio::io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt};

#[derive(Debug, Clone, Copy, Eq, PartialEq)]
pub enum MessageType {
    GetChunk = 0x01,
    PrefetchChunk = 0x02,
    RegisterChunk = 0x03,
    UnregisterChunk = 0x04,
    RegisterChunks = 0x05,
    UnregisterChunks = 0x06,
    HealthCheck = 0x07,
    StatsLocality = 0x08,
}

impl TryFrom<u8> for MessageType {
    type Error = io::Error;

    fn try_from(value: u8) -> io::Result<Self> {
        match value {
            0x01 => Ok(Self::GetChunk),
            0x02 => Ok(Self::PrefetchChunk),
            0x03 => Ok(Self::RegisterChunk),
            0x04 => Ok(Self::UnregisterChunk),
            0x05 => Ok(Self::RegisterChunks),
            0x06 => Ok(Self::UnregisterChunks),
            0x07 => Ok(Self::HealthCheck),
            0x08 => Ok(Self::StatsLocality),
            _ => Err(io::Error::new(
                ErrorKind::InvalidData,
                format!("invalid message type: {value}"),
            )),
        }
    }
}

#[derive(Debug, Clone, Copy, Eq, PartialEq)]
pub struct Request {
    pub request_id: u64,
    pub message_type: MessageType,
    pub checksum: CheckSum,
    pub offset: u32,
    pub length: u32,
}

impl Request {
    pub fn new(
        request_id: u64,
        message_type: MessageType,
        checksum: CheckSum,
        offset: u32,
        length: u32,
    ) -> Self {
        Self {
            request_id,
            message_type,
            checksum,
            offset,
            length,
        }
    }

    pub fn whole_chunk(message_type: MessageType, checksum: CheckSum) -> Self {
        Self::new(0, message_type, checksum, 0, 0)
    }

    pub fn control_batch(message_type: MessageType, count: usize) -> Self {
        Self::new(0, message_type, CheckSum::empty(), 0, count as u32)
    }

    pub(super) fn ensure_full_chunk(&self) -> io::Result<()> {
        if self.offset == 0 && self.length == 0 {
            return Ok(());
        }
        Err(io::Error::new(
            ErrorKind::InvalidInput,
            format!(
                "chunk peer protocol only supports full-chunk requests, got offset={} length={}",
                self.offset, self.length
            ),
        ))
    }

    pub(super) fn ensure_control_batch(&self) -> io::Result<usize> {
        if self.offset != 0 {
            return Err(io::Error::new(
                ErrorKind::InvalidInput,
                format!("invalid batch control request offset={}", self.offset),
            ));
        }
        Ok(self.length as usize)
    }

    pub(super) fn encode(&self) -> [u8; REQUEST_LEN] {
        let mut buf = [0_u8; REQUEST_LEN];
        buf[0..8].copy_from_slice(&self.request_id.to_be_bytes());
        buf[8] = self.message_type as u8;
        buf[9] = self.checksum.method.into();
        buf[10..42].copy_from_slice(&self.checksum.raw);
        buf[42..46].copy_from_slice(&self.offset.to_be_bytes());
        buf[46..50].copy_from_slice(&self.length.to_be_bytes());
        buf
    }

    pub(super) fn decode(buf: [u8; REQUEST_LEN]) -> io::Result<Self> {
        let request_id = u64::from_be_bytes(buf[0..8].try_into().unwrap());
        let message_type = MessageType::try_from(buf[8])?;
        let checksum = CheckSum::new(&buf[10..42], buf[9].into())?;
        let offset = u32::from_be_bytes(buf[42..46].try_into().unwrap());
        let length = u32::from_be_bytes(buf[46..50].try_into().unwrap());
        Ok(Self {
            request_id,
            message_type,
            checksum,
            offset,
            length,
        })
    }

    #[allow(dead_code)]
    pub(super) async fn read_from<R>(reader: &mut R) -> io::Result<Self>
    where
        R: AsyncRead + Unpin,
    {
        let mut buf = [0_u8; REQUEST_LEN];
        reader.read_exact(&mut buf).await?;
        Self::decode(buf)
    }

    #[allow(dead_code)]
    pub(super) async fn write_to<W>(&self, writer: &mut W) -> io::Result<()>
    where
        W: AsyncWrite + Unpin,
    {
        writer.write_all(&self.encode()).await
    }

    #[cfg(all(test, target_os = "linux"))]
    pub(super) fn write_to_sync<W: io::Write>(&self, writer: &mut W) -> io::Result<()> {
        writer.write_all(&self.encode())
    }
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub(super) struct WireResponse {
    pub(super) request_id: u64,
    pub(super) status: u8,
    pub(super) payload: Vec<u8>,
}

impl WireResponse {
    pub(super) async fn read_from<R>(reader: &mut R) -> io::Result<Self>
    where
        R: AsyncRead + Unpin,
    {
        let mut header = [0_u8; RESPONSE_HEADER_LEN];
        reader.read_exact(&mut header).await?;
        let request_id = u64::from_be_bytes(header[0..8].try_into().unwrap());
        let status = header[8];
        let payload_len = u32::from_be_bytes(header[9..13].try_into().unwrap()) as usize;
        if payload_len > MAX_CHUNK_PAYLOAD_SIZE {
            return Err(io::Error::new(
                ErrorKind::InvalidData,
                format!("payload too large: {payload_len}"),
            ));
        }
        let mut payload = vec![0_u8; payload_len];
        reader.read_exact(&mut payload).await?;
        Ok(Self {
            request_id,
            status,
            payload,
        })
    }

    #[cfg(test)]
    #[allow(dead_code)]
    pub(super) fn read_from_sync<R: Read>(reader: &mut R) -> io::Result<Self> {
        let mut header = [0_u8; RESPONSE_HEADER_LEN];
        reader.read_exact(&mut header)?;
        let request_id = u64::from_be_bytes(header[0..8].try_into().unwrap());
        let status = header[8];
        let payload_len = u32::from_be_bytes(header[9..13].try_into().unwrap()) as usize;
        if payload_len > MAX_CHUNK_PAYLOAD_SIZE {
            return Err(io::Error::new(
                ErrorKind::InvalidData,
                format!("payload too large: {payload_len}"),
            ));
        }
        let mut payload = vec![0_u8; payload_len];
        reader.read_exact(&mut payload)?;
        Ok(Self {
            request_id,
            status,
            payload,
        })
    }

    #[allow(dead_code)]
    pub(super) async fn write_to<W>(&self, writer: &mut W) -> io::Result<()>
    where
        W: AsyncWrite + Unpin,
    {
        let header = encode_response_header(self.request_id, self.status, self.payload.len());
        writer.write_all(&header).await?;
        writer.write_all(&self.payload).await
    }
}

pub(super) fn encode_response_header(
    request_id: u64,
    status: u8,
    payload_len: usize,
) -> [u8; RESPONSE_HEADER_LEN] {
    let mut header = [0_u8; RESPONSE_HEADER_LEN];
    header[0..8].copy_from_slice(&request_id.to_be_bytes());
    header[8] = status;
    header[9..13].copy_from_slice(&(payload_len as u32).to_be_bytes());
    header
}

pub(super) async fn read_checksum_batch<R>(
    reader: &mut R,
    count: usize,
) -> io::Result<Vec<CheckSum>>
where
    R: AsyncRead + Unpin,
{
    let mut checksums = Vec::with_capacity(count);
    let mut raw = [0_u8; 32];
    for _ in 0..count {
        let mut method = [0_u8; 1];
        reader.read_exact(&mut method).await?;
        reader.read_exact(&mut raw).await?;
        checksums.push(CheckSum::new(&raw, method[0].into())?);
    }
    Ok(checksums)
}
