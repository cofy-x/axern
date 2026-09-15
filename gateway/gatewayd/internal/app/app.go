package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/adapters/controlplane"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/adapters/nodebridge"
	controlapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/control"
	httpapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/http"
	nodeapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/node"
	sshapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/ssh"
	tunnelapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/tunnel"
	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
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
	controlClient, err := controlplane.Dial(ctx, cfg.ControlTarget, cfg.TLSCACert, cfg.WorkloadBundle, cfg.WorkloadCluster, cfg.ControlDialTimeout, obs.GRPCDialOptions()...)
	if err != nil {
		return nil, err
	}
	nodes, err := nodebridge.NewDialer(cfg.TLSCACert, cfg.WorkloadBundle, cfg.WorkloadCluster, obs)
	if err != nil {
		_ = controlClient.Close()
		return nil, err
	}
	closeDependencies := func() {
		_ = nodes.Close()
		_ = controlClient.Close()
	}
	metrics := observability.NewMetrics(obs)
	terminalManager := term.NewManager(controlClient, nodes, terminalOptions(cfg), metrics, obs)
	terminal := httpapi.NewTerminal(terminalManager, httpTerminalOptions(cfg), metrics)
	var sshServer *sshapi.Server
	if cfg.SSHEnabled {
		sshServer, err = sshapi.New(cfg.SSHAddress, cfg.SSHHostKey, terminalManager, metrics, obs)
		if err != nil {
			closeDependencies()
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
		if sshServer != nil {
			_ = sshServer.Close()
		}
		closeDependencies()
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
		controlServer.Close()
		if sshServer != nil {
			_ = sshServer.Close()
		}
		closeDependencies()
		return nil, err
	}
	controlServer.RegisterTunnelRelay(tunnelServer)
	controlServer.RegisterNodeSandbox(nodeapi.New(controlClient, nodes, nodeOptions(cfg), metrics))
	controlServer.RegisterHTTP(obs.HTTPHandler(httpapi.New(terminal), "gatewayd.terminal"))
	handler := httpapi.New(nil)
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
