use super::protocol::{encode_response_header, Request, WireResponse};
use super::{timeout_error, SESSION_MAX_INFLIGHT, STATUS_HIT};
use std::collections::HashMap;
use std::future::Future;
use std::io::{self, ErrorKind};
use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::io::{AsyncRead, AsyncWrite, AsyncWriteExt};
use tokio::net::{TcpStream, UnixStream};
use tokio::sync::{oneshot, Mutex, Notify};
use tokio::time::timeout;

#[derive(Debug, Clone, Copy)]
pub(super) struct ConnectionPoolConfig {
    pub(super) min_idle: usize,
    pub(super) max_size: usize,
    pub(super) idle_ttl: Duration,
}

impl Default for ConnectionPoolConfig {
    fn default() -> Self {
        Self {
            min_idle: super::CONNECTION_POOL_MIN_IDLE,
            max_size: super::CONNECTION_POOL_MAX_SIZE,
            idle_ttl: Duration::from_secs(super::CONNECTION_POOL_IDLE_TTL_SECS),
        }
    }
}

#[derive(Debug)]
struct SessionPoolState<S> {
    sessions: Vec<Arc<S>>,
}

impl<S> Default for SessionPoolState<S> {
    fn default() -> Self {
        Self {
            sessions: Vec::new(),
        }
    }
}

pub(super) trait PoolSession: Send + Sync + 'static {
    fn is_closed(&self) -> bool;
    fn inflight(&self) -> usize;
    fn last_used(&self) -> Instant;
    fn touch(&self);
}

#[derive(Debug)]
pub(super) struct SessionPool<S> {
    config: ConnectionPoolConfig,
    state: Mutex<SessionPoolState<S>>,
    notify: Notify,
    #[cfg(test)]
    connect_count: AtomicUsize,
}

impl<S> Default for SessionPool<S> {
    fn default() -> Self {
        Self {
            config: ConnectionPoolConfig::default(),
            state: Mutex::new(SessionPoolState::default()),
            notify: Notify::new(),
            #[cfg(test)]
            connect_count: AtomicUsize::new(0),
        }
    }
}

impl<S: PoolSession> SessionPool<S> {
    #[cfg(all(test, target_os = "linux"))]
    pub(super) fn with_config(config: ConnectionPoolConfig) -> Self {
        Self {
            config,
            state: Mutex::new(SessionPoolState::default()),
            notify: Notify::new(),
            #[cfg(test)]
            connect_count: AtomicUsize::new(0),
        }
    }

    fn config(&self) -> ConnectionPoolConfig {
        self.config
    }

    fn prune_idle_locked(state: &mut SessionPoolState<S>, config: ConnectionPoolConfig) {
        let now = Instant::now();
        let mut retained = Vec::with_capacity(state.sessions.len());
        let mut survivors = 0usize;
        for session in state.sessions.drain(..) {
            let expired = !session.is_closed()
                && session.inflight() == 0
                && now.duration_since(session.last_used()) > config.idle_ttl;
            if session.is_closed() || (expired && survivors >= config.min_idle) {
                continue;
            }
            survivors += 1;
            retained.push(session);
        }
        state.sessions = retained;
    }

    pub(super) async fn acquire<F, Fut>(self: &Arc<Self>, connect: F) -> io::Result<Arc<S>>
    where
        F: Fn() -> Fut,
        Fut: Future<Output = io::Result<Arc<S>>>,
    {
        loop {
            let config = self.config();
            let mut state = self.state.lock().await;
            Self::prune_idle_locked(&mut state, config);

            if let Some(session) = state
                .sessions
                .iter()
                .filter(|session| !session.is_closed())
                .min_by_key(|session| session.inflight())
                .cloned()
            {
                let should_create = state.sessions.len() < config.max_size
                    && session.inflight() >= SESSION_MAX_INFLIGHT;
                if !should_create {
                    session.touch();
                    return Ok(session);
                }
            }

            if state.sessions.len() < config.max_size {
                drop(state);
                match connect().await {
                    Ok(session) => {
                        #[cfg(test)]
                        self.connect_count.fetch_add(1, Ordering::Relaxed);
                        let mut state = self.state.lock().await;
                        state.sessions.push(Arc::clone(&session));
                        drop(state);
                        return Ok(session);
                    }
                    Err(err) => return Err(err),
                }
            }

            let notified = self.notify.notified();
            drop(state);
            notified.await;
        }
    }

    pub(super) async fn prune(&self) {
        let mut state = self.state.lock().await;
        Self::prune_idle_locked(&mut state, self.config());
        drop(state);
        self.notify.notify_one();
    }

    #[cfg(all(test, target_os = "linux"))]
    pub(super) fn connect_count(&self) -> usize {
        self.connect_count.load(Ordering::Relaxed)
    }

    #[cfg(all(test, target_os = "linux"))]
    pub(super) async fn state_counts(&self) -> (usize, usize) {
        let state = self.state.lock().await;
        let idle = state
            .sessions
            .iter()
            .filter(|session| session.inflight() == 0 && !session.is_closed())
            .count();
        (state.sessions.len(), idle)
    }
}

pub(super) type BoxAsyncReader = Box<dyn AsyncRead + Unpin + Send>;
pub(super) type BoxAsyncWriter = Box<dyn AsyncWrite + Unpin + Send>;
type PendingResponse = oneshot::Sender<io::Result<WireResponse>>;
type PendingMap = Arc<Mutex<HashMap<u64, PendingResponse>>>;

pub(super) enum ResponseWriter {
    Tcp(tokio::net::tcp::OwnedWriteHalf),
    Unix(tokio::net::unix::OwnedWriteHalf),
}

pub(super) trait RequestStream: Send + 'static {
    fn into_request_parts(self) -> (BoxAsyncReader, ResponseWriter);
}

impl RequestStream for TcpStream {
    fn into_request_parts(self) -> (BoxAsyncReader, ResponseWriter) {
        let (reader, writer) = self.into_split();
        (Box::new(reader), ResponseWriter::Tcp(writer))
    }
}

impl RequestStream for UnixStream {
    fn into_request_parts(self) -> (BoxAsyncReader, ResponseWriter) {
        let (reader, writer) = self.into_split();
        (Box::new(reader), ResponseWriter::Unix(writer))
    }
}

pub(super) struct MultiplexedSession {
    writer: Mutex<BoxAsyncWriter>,
    pending: PendingMap,
    next_request_id: AtomicU64,
    closed: AtomicBool,
    pub(super) inflight: AtomicUsize,
    last_used_micros: AtomicU64,
}

pub(super) type TcpConnPool = SessionPool<MultiplexedSession>;
pub(super) type UnixConnPool = SessionPool<MultiplexedSession>;
pub(super) type PeerPoolMap = Arc<Mutex<HashMap<SocketAddr, Arc<TcpConnPool>>>>;

impl PoolSession for MultiplexedSession {
    fn is_closed(&self) -> bool {
        self.closed.load(Ordering::Relaxed)
    }

    fn inflight(&self) -> usize {
        self.inflight.load(Ordering::Relaxed)
    }

    fn last_used(&self) -> Instant {
        super::instant_from_micros(self.last_used_micros.load(Ordering::Relaxed))
    }

    fn touch(&self) {
        self.last_used_micros
            .store(super::monotonic_now_micros(), Ordering::Relaxed);
    }
}

impl std::fmt::Debug for MultiplexedSession {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("MultiplexedSession")
            .field("closed", &self.closed.load(Ordering::Relaxed))
            .field("inflight", &self.inflight.load(Ordering::Relaxed))
            .finish()
    }
}

impl MultiplexedSession {
    #[cfg(all(test, target_os = "linux"))]
    pub(super) fn new<R>(reader: R, writer: BoxAsyncWriter) -> Arc<Self>
    where
        R: AsyncRead + Unpin + Send + 'static,
    {
        let session = Arc::new(Self {
            writer: Mutex::new(writer),
            pending: Arc::new(Mutex::new(HashMap::new())),
            next_request_id: AtomicU64::new(1),
            closed: AtomicBool::new(false),
            inflight: AtomicUsize::new(0),
            last_used_micros: AtomicU64::new(super::monotonic_now_micros()),
        });
        Self::spawn_reader(Arc::clone(&session), reader);
        session
    }

    pub(super) fn from_tcp(stream: TcpStream) -> Arc<Self> {
        let (reader, writer) = stream.into_split();
        Self::from_parts(reader, Box::new(writer))
    }

    pub(super) fn from_unix(stream: UnixStream) -> Arc<Self> {
        let (reader, writer) = stream.into_split();
        Self::from_parts(reader, Box::new(writer))
    }

    fn from_parts<R>(reader: R, writer: BoxAsyncWriter) -> Arc<Self>
    where
        R: AsyncRead + Unpin + Send + 'static,
    {
        let session = Arc::new(Self {
            writer: Mutex::new(writer),
            pending: Arc::new(Mutex::new(HashMap::new())),
            next_request_id: AtomicU64::new(1),
            closed: AtomicBool::new(false),
            inflight: AtomicUsize::new(0),
            last_used_micros: AtomicU64::new(super::monotonic_now_micros()),
        });
        Self::spawn_reader(Arc::clone(&session), reader);
        session
    }

    fn spawn_reader<R>(session: Arc<Self>, mut reader: R)
    where
        R: AsyncRead + Unpin + Send + 'static,
    {
        tokio::spawn(async move {
            loop {
                match WireResponse::read_from(&mut reader).await {
                    Ok(response) => {
                        session.touch();
                        let tx = session.pending.lock().await.remove(&response.request_id);
                        if let Some(tx) = tx {
                            let _ = tx.send(Ok(response));
                        } else {
                            session
                                .close(io::Error::new(
                                    ErrorKind::InvalidData,
                                    format!("unknown response request_id={}", response.request_id),
                                ))
                                .await;
                            return;
                        }
                    }
                    Err(err) => {
                        session.close(err).await;
                        return;
                    }
                }
            }
        });
    }

    #[cfg(all(test, target_os = "linux"))]
    pub(super) fn last_used(&self) -> Instant {
        PoolSession::last_used(self)
    }

    #[cfg(all(test, target_os = "linux"))]
    pub(super) fn inflight(&self) -> usize {
        PoolSession::inflight(self)
    }

    pub(super) fn touch(&self) {
        PoolSession::touch(self);
    }

    pub(super) async fn send_request(
        &self,
        mut request: Request,
        req_timeout: Duration,
    ) -> io::Result<WireResponse> {
        self.send_request_with_payload(&mut request, &[], req_timeout)
            .await
    }

    pub(super) async fn send_batch_request(
        &self,
        mut request: Request,
        payload: Vec<u8>,
        req_timeout: Duration,
    ) -> io::Result<WireResponse> {
        self.send_request_with_payload(&mut request, &payload, req_timeout)
            .await
    }

    async fn send_request_with_payload(
        &self,
        request: &mut Request,
        payload: &[u8],
        req_timeout: Duration,
    ) -> io::Result<WireResponse> {
        if self.is_closed() {
            return Err(io::Error::new(ErrorKind::BrokenPipe, "session is closed"));
        }

        let request_id = self.next_request_id.fetch_add(1, Ordering::Relaxed);
        request.request_id = request_id;
        let (tx, rx) = oneshot::channel();
        self.pending.lock().await.insert(request_id, tx);
        self.inflight.fetch_add(1, Ordering::Relaxed);
        self.touch();

        let write_res = {
            let mut writer = self.writer.lock().await;
            timeout(req_timeout, async {
                request.write_to(&mut *writer).await?;
                if !payload.is_empty() {
                    writer.write_all(payload).await?;
                }
                writer.flush().await
            })
            .await
        };
        match write_res {
            Ok(Ok(())) => {}
            Ok(Err(err)) => {
                self.pending.lock().await.remove(&request_id);
                self.inflight.fetch_sub(1, Ordering::Relaxed);
                self.close(io::Error::new(err.kind(), err.to_string()))
                    .await;
                return Err(err);
            }
            Err(_) => {
                self.pending.lock().await.remove(&request_id);
                self.inflight.fetch_sub(1, Ordering::Relaxed);
                let err = timeout_error("request write");
                self.close(io::Error::new(err.kind(), err.to_string()))
                    .await;
                return Err(err);
            }
        }

        let result = timeout(req_timeout, rx).await;
        self.inflight.fetch_sub(1, Ordering::Relaxed);
        self.touch();
        match result {
            Ok(Ok(Ok(response))) => Ok(response),
            Ok(Ok(Err(err))) => Err(err),
            Ok(Err(_)) => Err(io::Error::new(
                ErrorKind::BrokenPipe,
                "response channel closed",
            )),
            Err(_) => {
                self.pending.lock().await.remove(&request_id);
                let err = timeout_error("response read");
                self.close(io::Error::new(err.kind(), err.to_string()))
                    .await;
                Err(err)
            }
        }
    }

    async fn close(&self, err: io::Error) {
        if self.closed.swap(true, Ordering::Relaxed) {
            return;
        }
        let kind = err.kind();
        let message = err.to_string();
        let pending = std::mem::take(&mut *self.pending.lock().await);
        for (_, tx) in pending {
            let _ = tx.send(Err(io::Error::new(kind, message.clone())));
        }
    }
}

struct VectoredWriteCursor<'a> {
    header: &'a [u8],
    payload: &'a [u8],
    written: usize,
}

impl<'a> VectoredWriteCursor<'a> {
    fn new(header: &'a [u8], payload: &'a [u8]) -> Self {
        Self {
            header,
            payload,
            written: 0,
        }
    }

    fn is_empty(&self) -> bool {
        self.written >= self.header.len() + self.payload.len()
    }

    fn fill_io_slices<'s>(&self, scratch: &'s mut [io::IoSlice<'a>; 2]) -> &'s [io::IoSlice<'a>] {
        let mut count = 0;
        let header_len = self.header.len();
        if self.written < header_len {
            scratch[count] = io::IoSlice::new(&self.header[self.written..]);
            count += 1;
            scratch[count] = io::IoSlice::new(self.payload);
            count += 1;
            return &scratch[..count];
        }

        let payload_offset = self.written - header_len;
        if payload_offset < self.payload.len() {
            scratch[count] = io::IoSlice::new(&self.payload[payload_offset..]);
            count += 1;
        }
        &scratch[..count]
    }

    fn advance(&mut self, written: usize) {
        self.written = (self.written + written).min(self.header.len() + self.payload.len());
    }
}

impl ResponseWriter {
    pub(super) async fn write_response(&mut self, response: &WireResponse) -> io::Result<()> {
        self.write_response_parts(response.request_id, response.status, &response.payload)
            .await
    }

    pub(super) async fn write_get_chunk_hit(
        &mut self,
        request_id: u64,
        payload: &[u8],
    ) -> io::Result<()> {
        self.write_response_parts(request_id, STATUS_HIT, payload)
            .await
    }

    async fn write_response_parts(
        &mut self,
        request_id: u64,
        status: u8,
        payload: &[u8],
    ) -> io::Result<()> {
        let header = encode_response_header(request_id, status, payload.len());
        let mut cursor = VectoredWriteCursor::new(&header, payload);
        let mut scratch = [io::IoSlice::new(&[]), io::IoSlice::new(&[])];
        while !cursor.is_empty() {
            self.writable().await?;
            let slices = cursor.fill_io_slices(&mut scratch);
            match self.try_write_vectored(slices) {
                Ok(0) => {
                    return Err(io::Error::new(
                        ErrorKind::WriteZero,
                        "failed to write response to stream",
                    ));
                }
                Ok(written) => cursor.advance(written),
                Err(err) if err.kind() == ErrorKind::WouldBlock => {}
                Err(err) => return Err(err),
            }
        }
        self.flush().await
    }

    async fn writable(&self) -> io::Result<()> {
        match self {
            Self::Tcp(writer) => writer.writable().await,
            Self::Unix(writer) => writer.writable().await,
        }
    }

    fn try_write_vectored(&self, bufs: &[io::IoSlice<'_>]) -> io::Result<usize> {
        match self {
            Self::Tcp(writer) => writer.try_write_vectored(bufs),
            Self::Unix(writer) => writer.try_write_vectored(bufs),
        }
    }

    async fn flush(&mut self) -> io::Result<()> {
        match self {
            Self::Tcp(writer) => writer.flush().await,
            Self::Unix(writer) => writer.flush().await,
        }
    }
}
