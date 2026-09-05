package l7inspect

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"
)

// RelayHTTP authorizes every HTTP/1 message before opening or reusing an
// upstream connection. net/http owns request/response framing, including
// chunked bodies, trailers, Expect and pipelining. Bodies are streamed, not
// inspected. Upgrades are forbidden: they would escape per-message checks.
func RelayHTTP(conn net.Conn, timeout time.Duration, authorize func(HTTPRequest) bool, dial func(context.Context) (net.Conn, error)) {
	listener := &httpConnectionListener{conn: conn, done: make(chan struct{})}
	defer listener.Close()
	transport := &http.Transport{
		DialContext:        func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		DisableCompression: true,
		MaxIdleConns:       1, MaxIdleConnsPerHost: 1, MaxConnsPerHost: 1,
		IdleConnTimeout: timeout, ResponseHeaderTimeout: timeout,
		MaxResponseHeaderBytes: DefaultMaxHTTPHeaderBytes,
	}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Scheme = "http"
			p.Out.URL.Host = p.In.Host
		},
		Transport: transport,
		ModifyResponse: func(r *http.Response) error {
			if r.StatusCode == http.StatusSwitchingProtocols {
				return errors.New("upstream protocol upgrade denied")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	server := &http.Server{
		ReadHeaderTimeout: timeout, ReadTimeout: timeout, WriteTimeout: timeout, IdleTimeout: timeout,
		MaxHeaderBytes: DefaultMaxHTTPHeaderBytes,
		ErrorLog:       log.New(io.Discard, "", 0),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request, err := InspectHTTPRequest(r)
			if err != nil || request.DirectIP || !authorize(request) {
				w.Header().Set("Connection", "close")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			proxy.ServeHTTP(w, r)
		}),
	}
	_ = server.Serve(listener)
	_ = server.Close()
}

type httpConnectionListener struct {
	conn     net.Conn
	done     chan struct{}
	once     sync.Once
	accepted bool
}

func (l *httpConnectionListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return &httpRelayConnection{Conn: l.conn, close: l.Close}, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *httpConnectionListener) Addr() net.Addr { return l.conn.LocalAddr() }
func (l *httpConnectionListener) Close() error {
	l.once.Do(func() { _ = l.conn.Close(); close(l.done) })
	return nil
}

type httpRelayConnection struct {
	net.Conn
	close func() error
}

func (c *httpRelayConnection) Close() error { return c.close() }
