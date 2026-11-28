package preview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	axern "github.com/cofy-x/axern/sdk/go"
	"google.golang.org/grpc/codes"
)

const policy = "sandbox allow-scripts allow-same-origin; default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; frame-src 'none'; form-action 'none'; worker-src 'none'"

type Reader interface {
	PreviewHTTP(context.Context, uint32, string, string) (axern.HTTPPreview, error)
}

// Serve opens a fresh browser origin on the local client's loopback interface.
// Cancelling ctx retires the origin and aborts its in-flight requests.
func Serve(ctx context.Context, reader Reader, port uint32, announce func(string) error) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	host := net.JoinHostPort(hex.EncodeToString(nonce[:])+".localhost", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{
		Handler:           newHandler(reader, host, port, cancel),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      12 * time.Second,
		IdleTimeout:       20 * time.Second,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return serveCtx },
	}
	if err := announce("http://" + host + "/"); err != nil {
		return err
	}
	stop := context.AfterFunc(serveCtx, func() { _ = server.Close() })
	defer stop()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func newHandler(reader Reader, host string, port uint32, revoke func()) http.Handler {
	inflight := make(chan struct{}, 6)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", policy)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		if r.Host != host || r.URL.IsAbs() || r.URL.Host != "" || !localBrowserRequest(r) {
			http.NotFound(w, r)
			return
		}
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Preview supports GET and HEAD", http.StatusMethodNotAllowed)
			return
		}
		select {
		case inflight <- struct{}{}:
			defer func() { <-inflight }()
		default:
			http.Error(w, "Preview is busy", http.StatusTooManyRequests)
			return
		}
		response, err := reader.PreviewHTTP(r.Context(), port, r.Method, r.URL.RequestURI())
		if err != nil {
			writePreviewError(w, err, revoke)
			return
		}
		if response.Status < http.StatusOK || response.Status > 599 {
			http.Error(w, "Invalid preview response", http.StatusBadGateway)
			return
		}
		if response.Location != "" {
			if !localRedirect(response.Location) {
				http.Error(w, "Preview redirect is outside this origin", http.StatusBadGateway)
				return
			}
			w.Header().Set("Location", response.Location)
		}
		if mediaType, _, err := mime.ParseMediaType(response.ContentType); err != nil || mediaType == "" {
			response.ContentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", response.ContentType)
		w.WriteHeader(response.Status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(response.Body)
		}
	})
}

func localBrowserRequest(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "none", "same-origin":
		return true
	default:
		return false
	}
}

func localRedirect(location string) bool {
	target, err := url.ParseRequestURI(location)
	return err == nil && !target.IsAbs() && target.Host == "" &&
		strings.HasPrefix(target.Path, "/") && !strings.HasPrefix(target.Path, "//") &&
		!strings.ContainsAny(target.Path, "\\#")
}

func writePreviewError(w http.ResponseWriter, err error, revoke func()) {
	var rpc *axern.RPCError
	if errors.As(err, &rpc) {
		switch rpc.Code {
		case codes.Unauthenticated, codes.PermissionDenied, codes.NotFound:
			http.Error(w, "Preview access has ended", http.StatusGone)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			revoke()
			return
		case codes.FailedPrecondition:
			http.Error(w, "Preview is unavailable for this request", http.StatusGone)
			return
		case codes.DeadlineExceeded:
			http.Error(w, "Preview request timed out", http.StatusGatewayTimeout)
			return
		}
	}
	http.Error(w, "Preview service did not respond", http.StatusBadGateway)
}

// Validate bounds a temporary preview to a guest port and a finite lifetime.
func Validate(port uint32, ttl time.Duration) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("preview requires a guest port from 1 to 65535")
	}
	if ttl <= 0 || ttl > time.Hour {
		return fmt.Errorf("preview lifetime must be greater than zero and at most one hour")
	}
	return nil
}
