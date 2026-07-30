package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	artifactadapter "github.com/cofy-x/axern/gateway/gatewayd/internal/adapters/artifact"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/adapters/controlplane"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/adapters/nodebridge"
	artifactapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/artifact"
	controlapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/control"
	httpapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/http"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/api/http/dashboard"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/api/http/serviceproxy"
	nodeapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/node"
	sshapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/ssh"
	tunnelapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/tunnel"
	artifactapp "github.com/cofy-x/axern/gateway/gatewayd/internal/application/artifact"
	appservice "github.com/cofy-x/axern/gateway/gatewayd/internal/application/service"
	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/auth"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/config"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
)

type App struct {
	control *controlplane.Client
	nodes   *nodebridge.Dialer
	server  *http.Server
	edge    *controlapi.Server
	tunnel  *tunnelapi.Server
	ssh     *sshapi.Server
}

func New(ctx context.Context, cfg config.Config, obs *sdkobs.Handle) (*App, error) {
	controlClient, err := controlplane.Dial(ctx, cfg.ControlTarget, cfg.TLSCACert, cfg.TLSCert, cfg.TLSKey, cfg.ControlDialTimeout, obs.GRPCDialOptions()...)
	if err != nil {
		return nil, err
	}
	nodes := nodebridge.NewDialer(obs)
	token := auth.DevToken{Token: cfg.DevToken}
	metrics := observability.NewMetrics(obs)
	routeCache := appservice.NewCache(controlClient, appservice.Options{
		TTL:                   cfg.RouteCacheTTL,
		MaxEntries:            cfg.RouteCacheMaxEntries,
		EndpointQuarantineTTL: cfg.ServiceEndpointQuarantineTTL,
	}, metrics, obs)
	terminalManager := term.NewManager(controlClient, nodes, terminalOptions(cfg), metrics, obs)
	proxyHandler := serviceproxy.New(nodes, serviceProxyOptions(cfg), metrics, obs)
	terminal := httpapi.NewTerminal(token, terminalManager, httpTerminalOptions(cfg), metrics)
	var sshServer *sshapi.Server
	if cfg.SSHEnabled {
		sshServer, err = sshapi.New(cfg.SSHAddress, cfg.SSHHostKey, cfg.SSHAuthorizedKeys, terminalManager, metrics, obs)
		if err != nil {
			return nil, err
		}
	}
	controlServer, err := controlapi.New(controlClient.Conn(), controlapi.Options{
		Address: cfg.ControlEdgeAddress,
		CACert:  cfg.ControlEdgeTLSCACert,
		Cert:    cfg.ControlEdgeTLSCert,
		Key:     cfg.ControlEdgeTLSKey,
	}, obs)
	if err != nil {
		return nil, err
	}
	tunnelServer, err := tunnelapi.New(tunnelapi.Options{
		Target:      cfg.TunnelRelayTarget,
		Resolver:    controlClient,
		CACert:      cfg.TunnelRelayTLSCACert,
		ServerName:  cfg.TunnelRelayTLSServerName,
		DialTimeout: cfg.ControlDialTimeout,
		DialOptions: obs.GRPCDialOptions(),
	})
	if err != nil {
		return nil, err
	}
	controlServer.RegisterTunnelRelay(tunnelServer)
	controlServer.RegisterNodeSandbox(nodeapi.New(controlClient, nodes, nodeOptions(cfg), metrics))
	artifactService := artifactapp.New(controlClient, artifactadapter.New(cfg.ArtifactUpstreamTimeout), artifactapp.Options{MaxConcurrent: cfg.ArtifactMaxConcurrent, ChunkBytes: cfg.ArtifactChunkBytes, MaxBytes: cfg.ArtifactMaxBytes, Observer: metrics})
	controlServer.RegisterArtifactData(artifactapi.New(artifactService))
	var dashboardHandler *dashboard.Handler
	if cfg.DashboardEnabled {
		dashboardHandler, err = dashboard.New(token, cfg.DashboardVendorDir, dashboard.NewServiceReplicaResolver(controlClient.Gateway))
		if err != nil {
			return nil, err
		}
	}
	handler := httpapi.New(routeCache, proxyHandler, terminal, dashboardHandler, token, cfg.RequireHTTPAuth, metrics)
	wrappedHandler := obs.HTTPHandler(handler, "gatewayd.http")
	return &App{
		control: controlClient,
		nodes:   nodes,
		ssh:     sshServer,
		edge:    controlServer,
		tunnel:  tunnelServer,
		server: &http.Server{
			Addr:              cfg.HTTPAddress,
			Handler:           wrappedHandler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 3)
	go func() {
		errCh <- a.server.ListenAndServe()
	}()
	if a.edge != nil {
		go func() {
			errCh <- a.edge.Run(runCtx)
		}()
	}
	if a.ssh != nil {
		go func() {
			errCh <- a.ssh.Run(runCtx)
		}()
	}
	select {
	case <-ctx.Done():
		return a.shutdown()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return a.shutdown()
		}
		cancel()
		_ = a.shutdown()
		return err
	}
}

func (a *App) shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if a.ssh != nil {
		_ = a.ssh.Close()
	}
	if a.edge != nil {
		a.edge.Close()
	}
	if err := a.server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	if a.ssh != nil {
		_ = a.ssh.Close()
	}
	if a.edge != nil {
		a.edge.Close()
	}
	if a.nodes != nil {
		_ = a.nodes.Close()
	}
	if a.control != nil {
		return a.control.Close()
	}
	return nil
}
