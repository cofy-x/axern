package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	appdashboard "github.com/cofy-x/axern/apps/cli/internal/application/dashboard"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	var listen string
	var open bool
	var refresh time.Duration
	cmd := &cobra.Command{Use: "dashboard", Short: "Run the local read-only troubleshooting dashboard", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		listen = strings.TrimSpace(listen)
		if listen == "" {
			listen = "127.0.0.1:0"
		}
		if !isLoopbackListen(listen) {
			return command.Usage(fmt.Errorf("dashboard only supports loopback listen addresses"))
		}
		session, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer session.Close()
		listener, err := net.Listen("tcp", listen)
		if err != nil {
			return err
		}
		defer listener.Close()
		name, profile, _, _ := runtime.ResolveContext()
		links := appdashboard.LinksConfig{}
		if profile != nil {
			links.ContextName, links.ServiceURL = name, profile.ServiceURL
		}
		server := &server{dashboard: appdashboard.New(appdashboard.Clients{Service: session.Clients.Service, Tunnel: session.Clients.Tunnel, Quota: session.Clients.Quota, AllocationLifecycle: session.Clients.Admin, Audit: session.Clients.AdminAudit, Reliability: session.Clients.AdminReliability}), serviceClient: session.Clients.Service, linksConfig: links, refresh: refresh}
		httpServer := &http.Server{Handler: server.routes(), ReadHeaderTimeout: 5 * time.Second}
		errorsChannel := make(chan error, 1)
		go func() {
			err := httpServer.Serve(listener)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			errorsChannel <- err
		}()
		url := "http://" + listener.Addr().String()
		fmt.Fprintf(cmd.OutOrStdout(), "Axern dashboard: %s\n", url)
		if open {
			if err := openBrowser(url); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "could not open browser: %v\n", err)
			}
		}
		select {
		case err := <-errorsChannel:
			return err
		case <-session.Context.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdown)
			return session.Context.Err()
		}
	}}
	f := cmd.Flags()
	f.StringVar(&listen, "listen", "127.0.0.1:0", "local listen address")
	f.BoolVar(&open, "open", true, "open in the default browser")
	f.DurationVar(&refresh, "refresh", 5*time.Second, "default refresh interval")
	return cmd
}
