package llmproxy

import (
	"bytes"
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type Proxy struct {
	server   *http.Server
	ln       net.Listener
	recorder *Recorder
}

type Options struct {
	Upstream                *url.URL
	Provider                Provider
	UpstreamToken           string
	LocalToken              string
	DisableEnvironmentProxy bool
	Recorder                *Recorder
}

func New(options Options) (*Proxy, error) {
	if options.Upstream == nil {
		return nil, fmt.Errorf("upstream is required")
	}
	if options.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen managed proxy: %w", err)
	}
	handler := newReverseProxy(options)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
	}
	p := &Proxy{server: server, ln: ln, recorder: options.Recorder}
	go func() {
		_ = server.Serve(ln)
	}()
	return p, nil
}

func (p *Proxy) Addr() string {
	if p == nil || p.ln == nil {
		return ""
	}
	return p.ln.Addr().String()
}

func (p *Proxy) BaseURL() string {
	if p == nil {
		return ""
	}
	return "http://" + p.Addr()
}

func (p *Proxy) Close() error {
	if p == nil || p.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.server.Shutdown(ctx)
}

func newReverseProxy(options Options) http.Handler {
	target := *options.Upstream
	targetQuery := target.RawQuery
	proxy := &httputil.ReverseProxy{
		Transport:     recordingTransport{base: defaultTransport(options.DisableEnvironmentProxy), recorder: options.Recorder},
		FlushInterval: -1,
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = joinURLPath(target.Path, req.URL.Path)
			if targetQuery == "" || req.URL.RawQuery == "" {
				req.URL.RawQuery = targetQuery + req.URL.RawQuery
			} else {
				req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
			}
			req.Host = target.Host
			RemoveHopHeaders(req.Header)
			options.Provider.InjectAuth(req.Header, options.UpstreamToken)
		},
		ModifyResponse: func(resp *http.Response) error {
			RemoveHopHeaders(resp.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			http.Error(w, "managed proxy upstream error", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if options.LocalToken != "" && !localTokenAuthorized(options.Provider, req.Header, options.LocalToken) {
			http.Error(w, "managed proxy unauthorized", http.StatusUnauthorized)
			return
		}
		requestID := 0
		if options.Recorder != nil {
			requestID = options.Recorder.RecordRequest(req)
			if requestID > 0 {
				req = req.WithContext(context.WithValue(req.Context(), requestIDContextKey{}, requestID))
			}
		}
		proxy.ServeHTTP(w, req)
	})
}

type requestIDContextKey struct{}

type recordingTransport struct {
	base     http.RoundTripper
	recorder *Recorder
}

func (t recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	startedAt := time.Now()
	resp, err := base.RoundTrip(req)
	if err != nil {
		if t.recorder != nil {
			t.recorder.RecordError(req.Method, req.URL.Path, err)
		}
		return nil, err
	}
	if t.recorder == nil || resp == nil || resp.Body == nil {
		return resp, nil
	}
	requestID, _ := req.Context().Value(requestIDContextKey{}).(int)
	responseID := t.recorder.NextResponse()
	t.recorder.RecordResponseStart(req, resp, requestID, responseID)
	resp.Body = &recordingBody{
		body:       resp.Body,
		recorder:   t.recorder,
		req:        req,
		resp:       resp,
		requestID:  requestID,
		responseID: responseID,
		startedAt:  startedAt,
		streaming:  isStreamingResponse(resp),
	}
	return resp, nil
}

type recordingBody struct {
	body       io.ReadCloser
	recorder   *Recorder
	req        *http.Request
	resp       *http.Response
	requestID  int
	responseID int
	startedAt  time.Time
	streaming  bool

	buffer         bytes.Buffer
	eventBuffer    bytes.Buffer
	chunkIndex     int
	done           bool
	streamUsage    Usage
	hasStreamUsage bool
	totalBytes     int64
	streamOverflow bool
}

const (
	maxResponseCaptureBytes = 1 << 20
	maxStreamEventBytes     = 1 << 20
)

func (b *recordingBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		chunk := append([]byte(nil), p[:n]...)
		b.totalBytes += int64(n)
		remaining := maxResponseCaptureBytes - b.buffer.Len()
		if remaining > 0 {
			if remaining > len(chunk) {
				remaining = len(chunk)
			}
			_, _ = b.buffer.Write(chunk[:remaining])
		}
		if b.streaming {
			b.consumeStreamEvents(chunk)
		}
	}
	if err == io.EOF {
		b.finish()
	} else if err != nil && b.recorder != nil {
		b.recorder.RecordError(b.req.Method, b.req.URL.Path, err)
	}
	return n, err
}

func (b *recordingBody) Close() error {
	b.finish()
	return b.body.Close()
}

func (b *recordingBody) finish() {
	if b.done {
		return
	}
	b.done = true
	if b.streaming {
		b.flushStreamEvent()
	}
	if b.recorder != nil {
		var usage *Usage
		if b.streaming && b.hasStreamUsage {
			usage = &b.streamUsage
		}
		b.recorder.RecordResponseDone(b.req, b.resp, b.requestID, b.responseID, b.startedAt, b.buffer.Bytes(), b.totalBytes, usage)
	}
}

func (b *recordingBody) consumeStreamEvents(chunk []byte) {
	b.eventBuffer.Write(chunk)
	for {
		payload := b.eventBuffer.Bytes()
		index, delimiterSize := streamEventBoundary(payload)
		if index < 0 {
			if b.eventBuffer.Len() > maxStreamEventBytes {
				keep := 3
				if b.eventBuffer.Len() < keep {
					keep = b.eventBuffer.Len()
				}
				tail := append([]byte(nil), payload[len(payload)-keep:]...)
				b.eventBuffer.Reset()
				_, _ = b.eventBuffer.Write(tail)
				b.streamOverflow = true
			}
			return
		}
		event := append([]byte(nil), payload[:index+delimiterSize]...)
		b.eventBuffer.Next(index + delimiterSize)
		if b.streamOverflow {
			b.streamOverflow = false
			continue
		}
		b.recordStreamEvent(event)
	}
}

func streamEventBoundary(payload []byte) (int, int) {
	lf := bytes.Index(payload, []byte("\n\n"))
	crlf := bytes.Index(payload, []byte("\r\n\r\n"))
	if lf < 0 {
		return crlf, 4
	}
	if crlf < 0 || lf < crlf {
		return lf, 2
	}
	return crlf, 4
}

func (b *recordingBody) flushStreamEvent() {
	if b.eventBuffer.Len() == 0 || b.streamOverflow {
		b.eventBuffer.Reset()
		b.streamOverflow = false
		return
	}
	event := append([]byte(nil), b.eventBuffer.Bytes()...)
	b.eventBuffer.Reset()
	b.recordStreamEvent(event)
}

func (b *recordingBody) recordStreamEvent(event []byte) {
	if len(bytes.TrimSpace(event)) == 0 || b.recorder == nil {
		return
	}
	b.chunkIndex++
	if b.recorder.provider != nil {
		if usage := b.recorder.provider.ExtractUsage(event); usage != nil {
			b.streamUsage.InputTokens += usage.InputTokens
			b.streamUsage.OutputTokens += usage.OutputTokens
			b.streamUsage.CacheReadTokens += usage.CacheReadTokens
			b.streamUsage.TotalTokens += usage.TotalTokens
			b.hasStreamUsage = true
		}
	}
	b.recorder.RecordResponseChunk(b.req, b.requestID, b.responseID, b.chunkIndex, event)
}

func defaultTransport(disableEnvironmentProxy bool) http.RoundTripper {
	var proxy func(*http.Request) (*url.URL, error)
	if !disableEnvironmentProxy {
		proxy = http.ProxyFromEnvironment
	}
	return &http.Transport{
		Proxy:                 proxy,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}
}

func joinURLPath(basePath, requestPath string) string {
	if basePath == "" {
		if requestPath == "" {
			return "/"
		}
		return requestPath
	}
	if requestPath == "" || requestPath == "/" {
		return basePath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(requestPath, "/")
}

func isStreamingResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
}

func bearerTokenEqual(header string, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimPrefix(header, prefix)
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func localTokenAuthorized(provider Provider, header http.Header, token string) bool {
	if bearerTokenEqual(header.Get("Authorization"), token) {
		return true
	}
	if provider != nil && provider.Name() == "anthropic" {
		return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(header.Get("x-api-key"))), []byte(token)) == 1
	}
	return false
}
