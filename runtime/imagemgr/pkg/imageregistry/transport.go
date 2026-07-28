package imageregistry

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sirupsen/logrus"
)

const (
	maxRegistryConnsPerHost     = 32
	maxRegistryIdleConnsPerHost = 32
	maxRegistryIdleConns        = maxRegistryIdleConnsPerHost * 2
	registryDialTimeout         = 30 * time.Second
	registryTLSHandshakeTimeout = 30 * time.Second
	registryResponseTimeout     = 30 * time.Second
)

const dragonflyRegistryHeader = "X-Dragonfly-Registry"

func newRegistryBaseTransport(proxyURL string, direct bool) *http.Transport {
	transport := remote.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = maxRegistryConnsPerHost
	transport.MaxIdleConnsPerHost = maxRegistryIdleConnsPerHost
	transport.MaxIdleConns = maxRegistryIdleConns
	transport.TLSHandshakeTimeout = registryTLSHandshakeTimeout
	transport.ResponseHeaderTimeout = registryResponseTimeout
	transport.DialContext = (&net.Dialer{
		Timeout:   registryDialTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext

	if direct {
		transport.Proxy = nil
		return transport
	}

	if proxyURL == "" {
		return transport
	}

	proxyURLParsed, err := url.Parse(proxyURL)
	if err != nil {
		logrus.Warnf("Failed to parse proxy URL %s: %v", proxyURL, err)
		return transport
	}

	transport.Proxy = http.ProxyURL(proxyURLParsed)
	return transport
}

type registryMirrorTransport struct {
	base   http.RoundTripper
	mirror *url.URL
}

func (t *registryMirrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.base == nil || t.mirror == nil {
		return nil, fmt.Errorf("registry mirror transport is not configured")
	}
	if req.URL.Path != "/v2" && !strings.HasPrefix(req.URL.Path, "/v2/") {
		return t.base.RoundTrip(req)
	}
	cloned := req.Clone(req.Context())
	originScheme := req.URL.Scheme
	if originScheme == "" {
		originScheme = "https"
	}
	cloned.URL.Scheme = t.mirror.Scheme
	cloned.URL.Host = t.mirror.Host
	cloned.Host = t.mirror.Host
	cloned.Header.Set(dragonflyRegistryHeader, originScheme+"://"+req.URL.Host)
	return t.base.RoundTrip(cloned)
}

type registryTransport struct {
	base         *http.Transport
	roundTripper http.RoundTripper
	route        string
}

func (t *registryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	roundTripper := t.roundTripper
	if roundTripper == nil {
		roundTripper = t.base
	}

	var (
		resp *http.Response
		err  error
		conn net.Conn
	)

	for attempt := 0; attempt < registryFetchRetryAttempts; attempt++ {
		resp, err, conn = doRegistryRoundTrip(roundTripper, req)
		if !shouldRetryRegistryRequest(req, resp, err, attempt) {
			break
		}

		closeRegistryRetryState(resp, conn)
		delay := nextRegistryRetryDelay(attempt)
		logrus.Warnf(
			"retrying registry request %s %s after attempt %d/%d: %v",
			req.Method,
			req.URL.String(),
			attempt+1,
			registryFetchRetryAttempts,
			describeRegistryRetry(resp, err),
		)
		if sleepErr := waitForRegistryRetry(req.Context(), delay); sleepErr != nil {
			return nil, sleepErr
		}
	}

	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, err
	}

	if shouldCloseConnOnServerError(resp) && conn != nil {
		resp.Body = &closeConnBody{
			ReadCloser: resp.Body,
			conn:       conn,
		}
		resp.Close = true
	}

	return resp, nil
}

func doRegistryRoundTrip(roundTripper http.RoundTripper, req *http.Request) (*http.Response, error, net.Conn) {
	clonedReq, err := cloneRegistryRequest(req)
	if err != nil {
		return nil, err, nil
	}

	var conn net.Conn
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			conn = info.Conn
		},
	}
	clonedReq = clonedReq.WithContext(httptrace.WithClientTrace(clonedReq.Context(), trace))

	resp, err := roundTripper.RoundTrip(clonedReq)
	return resp, err, conn
}

func cloneRegistryRequest(req *http.Request) (*http.Request, error) {
	clonedReq := req.Clone(req.Context())
	switch {
	case req.Body == nil:
		return clonedReq, nil
	case req.GetBody == nil:
		return nil, fmt.Errorf("cannot retry registry request %s %s without GetBody", req.Method, req.URL.String())
	default:
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("failed to rewind registry request %s %s: %w", req.Method, req.URL.String(), err)
		}
		clonedReq.Body = body
		return clonedReq, nil
	}
}

type closeConnBody struct {
	io.ReadCloser
	conn      net.Conn
	closeOnce sync.Once
}

func (b *closeConnBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.closeConn()
	}
	return n, err
}

func (b *closeConnBody) Close() error {
	err := b.ReadCloser.Close()
	b.closeConn()
	return err
}

func (b *closeConnBody) closeConn() {
	b.closeOnce.Do(func() {
		_ = b.conn.Close()
	})
}
