package networking

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupDnatRulesBasic(t *testing.T) {
	fake := &fakeNetworkManager{}
	c := newTestCoordinator(t, fake)

	err := c.SetupDnatRules("ctr-1", []string{"tcp:8080:80", "udp:5353:53"}, "10.0.0.2")
	require.NoError(t, err)
	assert.Equal(t, []dnatCall{
		{"tcp", 8080, "10.0.0.2", 80},
		{"udp", 5353, "10.0.0.2", 53},
	}, fake.added)

	rules := c.DnatRules("ctr-1")
	require.Len(t, rules, 2)
	assert.Equal(t, "tcp", rules[0].Protocol)
	assert.Equal(t, uint16(8080), rules[0].DstPort)
	assert.Equal(t, "10.0.0.2", rules[0].TargetIP)
	assert.Equal(t, uint16(80), rules[0].TargetPort)
}

func TestParseDnatRulesSkipsEmptyEntries(t *testing.T) {
	rules, err := ParseDnatRules("ctr-1", []string{"", "tcp:8080:80"}, "10.0.0.2")
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, "tcp", rules[0].Protocol)
	assert.Equal(t, uint16(8080), rules[0].DstPort)
	assert.Equal(t, uint16(80), rules[0].TargetPort)
}

func TestSetupDnatRulesEmptyPorts(t *testing.T) {
	fake := &fakeNetworkManager{}
	c := newTestCoordinator(t, fake)

	err := c.SetupDnatRules("ctr-1", nil, "10.0.0.2")
	require.NoError(t, err)
	assert.Empty(t, fake.added)
}

func TestSetupDnatRulesInvalidFormat(t *testing.T) {
	c := newTestCoordinator(t, &fakeNetworkManager{})

	err := c.SetupDnatRules("ctr-1", []string{"tcp:8080"}, "10.0.0.2")
	assert.ErrorContains(t, err, "invalid port format")

	err = c.SetupDnatRules("ctr-1", []string{"tcp:notanumber:80"}, "10.0.0.2")
	assert.ErrorContains(t, err, "invalid dstPort")

	err = c.SetupDnatRules("ctr-1", []string{"tcp:8080:notanumber"}, "10.0.0.2")
	assert.ErrorContains(t, err, "invalid targetPort")
}

func TestSetupDnatRulesNetworkManagerErrorDoesNotRecordState(t *testing.T) {
	fake := &fakeNetworkManager{failNext: true}
	c := newTestCoordinator(t, fake)

	err := c.SetupDnatRules("ctr-1", []string{"tcp:8080:80"}, "10.0.0.2")
	assert.ErrorContains(t, err, "failed to add DNAT rule")
	assert.Empty(t, c.DnatRules("ctr-1"))
}

func TestSetupDnatRulesRollsBackPartialInstall(t *testing.T) {
	fake := &fakeNetworkManager{failSetupCall: 2}
	c := newTestCoordinator(t, fake)

	err := c.SetupDnatRules("ctr-1", []string{"tcp:8080:80", "tcp:9090:90"}, "10.0.0.2")
	require.Error(t, err)
	assert.Equal(t, []dnatCall{{"tcp", 8080, "10.0.0.2", 80}}, fake.added)
	assert.Equal(t, []dnatCall{{"tcp", 8080, "10.0.0.2", 80}}, fake.removed)
	assert.Empty(t, c.DnatRules("ctr-1"))
}

func TestSetupDnatRulesNoNetworkManager(t *testing.T) {
	c := NewCoordinator(Options{
		NatBackend:     "missing",
		NetworkManager: func(string) (networkmanager.NetworkManager, bool) { return nil, false },
	})

	err := c.SetupDnatRules("ctr-1", []string{"tcp:8080:80"}, "10.0.0.2")
	assert.ErrorContains(t, err, "network manager not found")
}

func TestCleanupDnatRulesBasic(t *testing.T) {
	fake := &fakeNetworkManager{}
	c := newTestCoordinator(t, fake)
	require.NoError(t, c.SetupDnatRules("ctr-1", []string{"tcp:8080:80", "udp:5353:53"}, "10.0.0.2"))

	c.CleanupDnatRules("ctr-1")

	assert.Equal(t, []dnatCall{
		{"tcp", 8080, "10.0.0.2", 80},
		{"udp", 5353, "10.0.0.2", 53},
	}, fake.removed)
	assert.Empty(t, c.DnatRules("ctr-1"))
}

func TestCleanupDnatRulesRetainsFailedRulesForRetry(t *testing.T) {
	fake := &fakeNetworkManager{}
	c := newTestCoordinator(t, fake)
	require.NoError(t, c.SetupDnatRules("ctr-1", []string{"tcp:8080:80"}, "10.0.0.2"))
	fake.failNext = true

	c.CleanupDnatRules("ctr-1")
	require.Len(t, c.DnatRules("ctr-1"), 1)

	c.CleanupDnatRules("ctr-1")
	assert.Empty(t, c.DnatRules("ctr-1"))
}

func TestCleanupDnatRulesNonexistentContainer(t *testing.T) {
	fake := &fakeNetworkManager{}
	c := newTestCoordinator(t, fake)

	c.CleanupDnatRules("ctr-nonexistent")
	assert.Empty(t, fake.removed)
}

func TestStoreDnatRulesPersists(t *testing.T) {
	mockStore := storetest.NewMockStore()
	c := newTestCoordinatorWithStore(t, &fakeNetworkManager{}, mockStore)

	require.NoError(t, c.SetupDnatRules("ctr-persist-1", []string{"tcp:9090:90", "udp:5353:53"}, "10.0.0.5"))

	var stored runtime.Map
	require.NoError(t, mockStore.LoadSnapshot(config.DNATRulesBucket, &stored))
	require.Contains(t, stored.GetItems(), "ctr-persist-1")

	c.CleanupDnatRules("ctr-persist-1")
	assert.Zero(t, c.DnatRuleCount())
}

func TestSetupDnatRulesMultipleContainers(t *testing.T) {
	c := newTestCoordinator(t, &fakeNetworkManager{})

	require.NoError(t, c.SetupDnatRules("ctr-1", []string{"tcp:8080:80"}, "10.0.0.2"))
	require.NoError(t, c.SetupDnatRules("ctr-2", []string{"tcp:9090:90"}, "10.0.0.3"))

	assert.Equal(t, 2, c.DnatRuleCount())
	c.CleanupDnatRules("ctr-1")
	assert.Empty(t, c.DnatRules("ctr-1"))
	assert.NotEmpty(t, c.DnatRules("ctr-2"))
}

func TestLoadDnatRulesEmptyStore(t *testing.T) {
	c := newTestCoordinator(t, &fakeNetworkManager{})

	c.LoadDnatRules()

	assert.Zero(t, c.DnatRuleCount())
}

func TestLoadDnatRulesReconcilesEmptyDesiredState(t *testing.T) {
	fake := &reconcilingNetworkManager{fakeNetworkManager: &fakeNetworkManager{}}
	c := NewCoordinator(Options{
		NatBackend:     "test",
		Store:          storetest.NewMockStore(),
		NetworkManager: func(string) (networkmanager.NetworkManager, bool) { return fake, true },
	})

	c.LoadDnatRules()

	assert.Equal(t, 1, fake.reconcileCalls)
	assert.Empty(t, fake.desired)
}

func TestLoadDnatRulesRestoresActiveResourceBackedRules(t *testing.T) {
	mockStore := storetest.NewMockStore()
	resources := map[string]container.OccupiedResource{
		"active": newNetworkResource("active", "10.0.0.8", "/var/run/netns/active"),
	}
	c := newTestCoordinatorWithStore(t, &fakeNetworkManager{}, mockStore)
	require.NoError(t, c.SetupDnatRules("active", []string{"tcp:18080:80"}, "10.0.0.8"))

	reloaded := NewCoordinator(Options{
		NatBackend: "test",
		Store:      mockStore,
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			resource, ok := resources[id]
			if !ok {
				return container.OccupiedResource{}, fmt.Errorf("missing resource")
			}
			return resource, nil
		},
		ContainerExists: func(id string) bool { return id == "active" },
		NetworkManager:  func(string) (networkmanager.NetworkManager, bool) { return &fakeNetworkManager{}, true },
	})

	reloaded.LoadDnatRules()

	require.Len(t, reloaded.DnatRules("active"), 1)
	assert.Equal(t, uint16(18080), reloaded.DnatRules("active")[0].DstPort)
}

func TestLoadDnatRulesReconcilesOnlyLiveContainerRules(t *testing.T) {
	mockStore := storetest.NewMockStore()
	writer := newTestCoordinatorWithStore(t, &fakeNetworkManager{}, mockStore)
	require.NoError(t, writer.SetupDnatRules("active", []string{"tcp:18080:80"}, "10.0.0.8"))
	require.NoError(t, writer.SetupDnatRules("stale", []string{"tcp:19090:90"}, "10.0.0.9"))

	fake := &reconcilingNetworkManager{fakeNetworkManager: &fakeNetworkManager{}}
	reloaded := NewCoordinator(Options{
		NatBackend: "test",
		Store:      mockStore,
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.8", "/var/run/netns/"+id), nil
		},
		ContainerExists: func(id string) bool { return id == "active" },
		NetworkManager:  func(string) (networkmanager.NetworkManager, bool) { return fake, true },
	})

	reloaded.LoadDnatRules()

	require.Equal(t, []networkmanager.DNATRule{{
		Protocol: "tcp", HostPort: 18080, TargetIP: "10.0.0.8", TargetPort: 80,
	}}, fake.desired)
	assert.Empty(t, reloaded.DnatRules("stale"))
	var stored runtime.Map
	require.NoError(t, mockStore.LoadSnapshot(config.DNATRulesBucket, &stored))
	assert.Contains(t, stored.Items, "active")
	assert.NotContains(t, stored.Items, "stale")
}

func TestNetworkForSandboxReturnsResourceAndRuntimeClass(t *testing.T) {
	c := NewCoordinator(Options{
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.9", "/var/run/netns/ctr"), nil
		},
		RuntimeClass: func(string) (string, error) { return "runsc", nil },
		NetworkManager: func(string) (networkmanager.NetworkManager, bool) {
			return &fakeNetworkManager{}, true
		},
	})

	network, err := c.NetworkForSandbox("ctr")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.9", network.IP)
	assert.Equal(t, "/var/run/netns/ctr", network.NetNSPath)
	assert.Equal(t, "runsc", network.RuntimeClass)
}

func TestCleanupActivationNetwork(t *testing.T) {
	fake := &fakeNetworkManager{}
	c := newTestCoordinator(t, fake)

	err := c.CleanupActivationNetwork(newNetworkResource("ctr", "10.0.0.10", "/var/run/netns/ctr"))
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.10"}, fake.activationCleanups)
}

func TestProxyHTTPForwardsRequestAndResponse(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	stream := newFakeHTTPProxyStream("ctr", 8080, http.MethodPost, "/hello", "q=1", [][]byte{[]byte("ping")})
	stream.header.Set("X-Test", "yes")
	c := NewCoordinator(Options{
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.11", "/var/run/netns/ctr"), nil
		},
		NetworkManager: func(string) (networkmanager.NetworkManager, bool) { return &fakeNetworkManager{}, true },
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
	})
	done := make(chan error, 1)
	go func() {
		req, err := http.ReadRequest(bufio.NewReader(serverConn))
		if err != nil {
			done <- err
			return
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			done <- err
			return
		}
		if req.Method != http.MethodPost || req.URL.RequestURI() != "/hello?q=1" || req.Header.Get("X-Test") != "yes" || string(body) != "ping" {
			done <- fmt.Errorf("unexpected request method=%s uri=%s header=%q body=%q", req.Method, req.URL.RequestURI(), req.Header.Get("X-Test"), string(body))
			return
		}
		_, err = serverConn.Write([]byte("HTTP/1.1 201 Created\r\nX-Upstream: ok\r\nContent-Length: 4\r\n\r\npong"))
		_ = serverConn.Close()
		done <- err
	}()

	err := c.ProxyHTTP(stream)
	require.NoError(t, err)
	require.NoError(t, <-done)
	assert.Equal(t, 201, stream.statusCode)
	assert.Equal(t, "ok", stream.responseHeader.Get("X-Upstream"))
	assert.Equal(t, [][]byte{[]byte("pong")}, stream.responseBody)
}

func TestProxyHTTPRetriesTransientConnectFailure(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	stream := newFakeHTTPProxyStream("ctr", 8080, http.MethodPost, "/hello", "", [][]byte{[]byte("ping")})
	stream.ctx, stream.cancel = context.WithTimeout(context.Background(), time.Second)
	defer stream.cancel()
	attempts := 0
	c := NewCoordinator(Options{
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.11", "/var/run/netns/ctr"), nil
		},
		NetworkManager:    func(string) (networkmanager.NetworkManager, bool) { return &fakeNetworkManager{}, true },
		ConnectRetryDelay: time.Millisecond,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			attempts++
			if attempts == 1 {
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("i/o timeout")}
			}
			return clientConn, nil
		},
	})
	done := make(chan error, 1)
	go func() {
		req, err := http.ReadRequest(bufio.NewReader(serverConn))
		if err != nil {
			done <- err
			return
		}
		_, _ = io.Copy(io.Discard, req.Body)
		_, err = serverConn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\npong"))
		_ = serverConn.Close()
		done <- err
	}()

	err := c.ProxyHTTP(stream)
	require.NoError(t, err)
	require.NoError(t, <-done)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, [][]byte{[]byte("pong")}, stream.responseBody)
}

func TestProxyHTTPNoBodyDoesNotSendChunkedBody(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	stream := newFakeHTTPProxyStream("ctr", 8080, http.MethodGet, "/", "", nil)
	c := NewCoordinator(Options{
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.11", "/var/run/netns/ctr"), nil
		},
		NetworkManager: func(string) (networkmanager.NetworkManager, bool) { return &fakeNetworkManager{}, true },
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
	})
	done := make(chan error, 1)
	go func() {
		req, err := http.ReadRequest(bufio.NewReader(serverConn))
		if err != nil {
			done <- err
			return
		}
		if len(req.TransferEncoding) > 0 || req.ContentLength != 0 {
			done <- fmt.Errorf("unexpected request body framing transfer=%v content_length=%d", req.TransferEncoding, req.ContentLength)
			return
		}
		_, err = serverConn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"))
		_ = serverConn.Close()
		done <- err
	}()

	err := c.ProxyHTTP(stream)
	require.NoError(t, err)
	require.NoError(t, <-done)
	assert.Equal(t, [][]byte{[]byte("ok")}, stream.responseBody)
}

func TestCloseHTTPProxyTransportsRemovesAllocationPools(t *testing.T) {
	c := NewCoordinator(Options{})
	first := c.httpProxyTransport("alloc-a", 8080)
	second := c.httpProxyTransport("alloc-a", 9090)
	other := c.httpProxyTransport("alloc-b", 8080)
	require.Same(t, first, c.httpProxyTransport("alloc-a", 8080))
	require.Len(t, c.proxies, 3)

	c.CloseHTTPProxyTransports("alloc-a")

	require.Len(t, c.proxies, 1)
	require.Same(t, other, c.proxies[httpProxyTransportKey("alloc-b", 8080)])
	require.NotSame(t, first, c.httpProxyTransport("alloc-a", 8080))
	require.NotSame(t, second, c.httpProxyTransport("alloc-a", 9090))

	c.CloseHTTPProxyTransports("alloc-a")
	require.Len(t, c.proxies, 1)
}

func TestProbePortConnectsAndClosesAllocationPort(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	closed := make(chan struct{})
	c := NewCoordinator(Options{
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.11", "/var/run/netns/ctr"), nil
		},
		NetworkManager: func(string) (networkmanager.NetworkManager, bool) { return &fakeNetworkManager{}, true },
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != "10.0.0.11:8080" {
				return nil, fmt.Errorf("dial %s %s", network, address)
			}
			return clientConn, nil
		},
	})
	go func() {
		buf := make([]byte, 1)
		_, _ = serverConn.Read(buf)
		_ = serverConn.Close()
		close(closed)
	}()

	err := c.ProbePort(context.Background(), "ctr", 8080)
	require.NoError(t, err)
	<-closed
}

type fakeNetworkManager struct {
	mu                 sync.Mutex
	added              []dnatCall
	removed            []dnatCall
	activationCleanups []string
	failNext           bool
	setupCalls         int
	failSetupCall      int
}

type reconcilingNetworkManager struct {
	*fakeNetworkManager
	desired        []networkmanager.DNATRule
	reconcileCalls int
}

func (f *reconcilingNetworkManager) ReconcileDNATRules(desired []networkmanager.DNATRule) error {
	f.reconcileCalls++
	f.desired = append([]networkmanager.DNATRule(nil), desired...)
	return nil
}

type dnatCall struct {
	Protocol   string
	DstPort    uint16
	TargetIP   string
	TargetPort uint16
}

func (f *fakeNetworkManager) SetupSNATRules(string) error                         { return nil }
func (f *fakeNetworkManager) CleanupSNATRules(string) error                       { return nil }
func (f *fakeNetworkManager) SetupNetworkRulesForActivating(net.IP, string) error { return nil }

func (f *fakeNetworkManager) CleanupNetworkRulesForActivating(ip net.IP) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activationCleanups = append(f.activationCleanups, ip.String())
	return nil
}

func (f *fakeNetworkManager) SetupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setupCalls++
	if f.failSetupCall > 0 && f.setupCalls == f.failSetupCall {
		return fmt.Errorf("injected setup error")
	}
	if f.failNext {
		f.failNext = false
		return fmt.Errorf("injected error")
	}
	f.added = append(f.added, dnatCall{protocol, dstPort, targetIP, targetPort})
	return nil
}

func (f *fakeNetworkManager) CleanupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return fmt.Errorf("injected error")
	}
	f.removed = append(f.removed, dnatCall{protocol, dstPort, targetIP, targetPort})
	return nil
}

func newTestCoordinator(t *testing.T, fake *fakeNetworkManager) *Coordinator {
	t.Helper()
	return newTestCoordinatorWithStore(t, fake, storetest.NewMockStore())
}

func newTestCoordinatorWithStore(t *testing.T, fake *fakeNetworkManager, dbStore *storetest.MockStore) *Coordinator {
	t.Helper()
	return NewCoordinator(Options{
		NatBackend: "test",
		Store:      dbStore,
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.2", "/var/run/netns/"+id), nil
		},
		ContainerExists: func(string) bool { return true },
		RuntimeClass:    func(string) (string, error) { return "runsc", nil },
		NetworkManager:  func(string) (networkmanager.NetworkManager, bool) { return fake, true },
	})
}

func newNetworkResource(id, ip, netns string) container.OccupiedResource {
	netResource := &resourcemanager.NetResource{Ip: net.ParseIP(ip), NetNSPath: netns}
	return container.OccupiedResource{
		ID: id,
		Resources: map[resourcemanager.ResourceName]string{
			resourcemanager.InterfaceResourceName: netResource.ToString(),
		},
	}
}

type fakeHTTPProxyStream struct {
	id             string
	port           int32
	method         string
	path           string
	query          string
	header         http.Header
	hasBody        bool
	contentLength  int64
	ctx            context.Context
	cancel         context.CancelFunc
	inputs         [][]byte
	statusCode     int
	responseHeader http.Header
	responseBody   [][]byte
	trailers       http.Header
}

func newFakeHTTPProxyStream(id string, port int32, method, path, query string, inputs [][]byte) *fakeHTTPProxyStream {
	return &fakeHTTPProxyStream{
		id:             id,
		port:           port,
		method:         method,
		path:           path,
		query:          query,
		header:         make(http.Header),
		hasBody:        len(inputs) > 0,
		contentLength:  int64(totalBytes(inputs)),
		ctx:            context.Background(),
		inputs:         inputs,
		responseHeader: make(http.Header),
		trailers:       make(http.Header),
	}
}

func (f *fakeHTTPProxyStream) TargetID() string         { return f.id }
func (f *fakeHTTPProxyStream) Port() int32              { return f.port }
func (f *fakeHTTPProxyStream) Method() string           { return f.method }
func (f *fakeHTTPProxyStream) Path() string             { return f.path }
func (f *fakeHTTPProxyStream) Query() string            { return f.query }
func (f *fakeHTTPProxyStream) Header() http.Header      { return f.header }
func (f *fakeHTTPProxyStream) HasBody() bool            { return f.hasBody }
func (f *fakeHTTPProxyStream) ContentLength() int64     { return f.contentLength }
func (f *fakeHTTPProxyStream) Context() context.Context { return f.ctx }

func (f *fakeHTTPProxyStream) RecvBody() ([]byte, error) {
	if len(f.inputs) == 0 {
		time.Sleep(10 * time.Millisecond)
		return nil, io.EOF
	}
	next := f.inputs[0]
	f.inputs = f.inputs[1:]
	return next, nil
}

func (f *fakeHTTPProxyStream) SendHead(statusCode int, header http.Header) error {
	f.statusCode = statusCode
	f.responseHeader = header.Clone()
	return nil
}

func (f *fakeHTTPProxyStream) SendBody(data []byte) error {
	f.responseBody = append(f.responseBody, append([]byte(nil), data...))
	return nil
}

func (f *fakeHTTPProxyStream) SendTrailers(header http.Header) error {
	f.trailers = header.Clone()
	return nil
}

func totalBytes(chunks [][]byte) int {
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	return total
}
