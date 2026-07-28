package networking

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type stateStore interface {
	SaveSnapshot(bucket string, value proto.Message) error
	LoadSnapshot(bucket string, value proto.Message) error
}

type Options struct {
	NatBackend          string
	Store               stateStore
	CollectResourceByID func(id string) (container.OccupiedResource, error)
	ContainerExists     func(id string) bool
	RuntimeClass        func(id string) (string, error)
	NetworkManager      func(name string) (networkmanager.NetworkManager, bool)
	DialContext         func(ctx context.Context, network, address string) (net.Conn, error)
	ConnectTimeout      time.Duration
	ConnectRetryDelay   time.Duration
	Logger              logrus.FieldLogger
}

type Coordinator struct {
	natBackend          string
	store               stateStore
	collectResourceByID func(id string) (container.OccupiedResource, error)
	containerExists     func(id string) bool
	runtimeClass        func(id string) (string, error)
	networkManager      func(name string) (networkmanager.NetworkManager, bool)
	dialContext         func(ctx context.Context, network, address string) (net.Conn, error)
	connectTimeout      time.Duration
	connectRetryDelay   time.Duration
	logger              logrus.FieldLogger

	dnatMu    sync.Mutex
	dnatRules map[string][]*DnatRule
	proxyMu   sync.Mutex
	proxies   map[string]*http.Transport
}

const (
	defaultConnectTimeout    = 5 * time.Second
	defaultDialAttempt       = 1 * time.Second
	defaultConnectRetryDelay = 200 * time.Millisecond
)

func NewCoordinator(options Options) *Coordinator {
	manager := options.NetworkManager
	if manager == nil {
		manager = func(name string) (networkmanager.NetworkManager, bool) {
			m, ok := networkmanager.NetworkManagers[name]
			return m, ok
		}
	}
	dialContext := options.DialContext
	if dialContext == nil {
		dialer := net.Dialer{Timeout: defaultDialAttempt}
		dialContext = dialer.DialContext
	}
	connectTimeout := options.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = defaultConnectTimeout
	}
	connectRetryDelay := options.ConnectRetryDelay
	if connectRetryDelay <= 0 {
		connectRetryDelay = defaultConnectRetryDelay
	}
	logger := options.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Coordinator{
		natBackend:          options.NatBackend,
		store:               options.Store,
		collectResourceByID: options.CollectResourceByID,
		containerExists:     options.ContainerExists,
		runtimeClass:        options.RuntimeClass,
		networkManager:      manager,
		dialContext:         dialContext,
		connectTimeout:      connectTimeout,
		connectRetryDelay:   connectRetryDelay,
		logger:              logger,
		dnatRules:           make(map[string][]*DnatRule),
		proxies:             make(map[string]*http.Transport),
	}
}
