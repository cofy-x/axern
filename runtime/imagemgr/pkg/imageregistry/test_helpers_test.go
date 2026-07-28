package imageregistry

import (
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type testConn struct {
	closeCount atomic.Int32
}

func (c *testConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *testConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *testConn) Close() error                     { c.closeCount.Add(1); return nil }
func (c *testConn) LocalAddr() net.Addr              { return testAddr("local") }
func (c *testConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (c *testConn) SetDeadline(time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(time.Time) error { return nil }

type testTimeoutError struct {
	msg string
}

func (e testTimeoutError) Error() string   { return e.msg }
func (e testTimeoutError) Timeout() bool   { return true }
func (e testTimeoutError) Temporary() bool { return true }

func setRegistryRetryDelays(t *testing.T, delay time.Duration, maxDelay time.Duration) func() {
	t.Helper()

	prevDelay := registryRetryDelay
	prevMaxDelay := registryRetryMaxDelay
	registryRetryDelay = delay
	registryRetryMaxDelay = maxDelay

	return func() {
		registryRetryDelay = prevDelay
		registryRetryMaxDelay = prevMaxDelay
	}
}

type testAddr string

func (a testAddr) Network() string { return string(a) }
func (a testAddr) String() string  { return string(a) }
