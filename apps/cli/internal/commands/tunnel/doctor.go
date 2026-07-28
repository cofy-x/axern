package tunnelcmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/tunnelrelay"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"github.com/spf13/cobra"
)

func doctorCommand(runtime command.Runtime) *cobra.Command {
	var sessionID, allocationID, serviceID, localTarget string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose tunnel control, relay, session, and local upstream readiness",
		Args:  command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			selected := 0
			for _, value := range []string{sessionID, allocationID, serviceID} {
				if strings.TrimSpace(value) != "" {
					selected++
				}
			}
			if selected != 1 {
				return command.Usage(fmt.Errorf("exactly one of --session-id, --allocation-id, or --service-id is required"))
			}
			if timeout <= 0 {
				return command.Usage(fmt.Errorf("--check-timeout must be positive"))
			}
			connection, err := runtime.ConnectionConfig()
			if err != nil {
				return command.Usage(err)
			}
			session, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer session.Close()
			relay := tunnelrelay.Config(connection)
			report, err := apptunnel.New(session.Clients.Tunnel).Doctor(session.Context, apptunnel.DoctorParams{
				SessionID: strings.TrimSpace(sessionID), AllocationID: strings.TrimSpace(allocationID), ServiceID: strings.TrimSpace(serviceID),
				LocalTarget: strings.TrimSpace(localTarget), Timeout: timeout, ServiceClient: session.Clients.Service,
				ProbeRelay: func(ctx context.Context, target string, timeout time.Duration) bool {
					return probeRelay(ctx, target, relay, timeout)
				},
			})
			if err != nil {
				return err
			}
			if err := renderDoctor(cmd, runtime, report, localTarget); err != nil {
				return err
			}
			if len(report.Problems) > 0 {
				return command.ExitError{Code: 1, Err: fmt.Errorf("tunnel doctor found problems")}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&sessionID, "session-id", "", "existing tunnel session id")
	f.StringVar(&allocationID, "allocation-id", "", "allocation whose newest active session should be inspected")
	f.StringVar(&serviceID, "service-id", "", "service whose selected ready allocation tunnel should be inspected")
	f.StringVar(&localTarget, "local", "", "optional local upstream host:port to probe")
	f.DurationVar(&timeout, "check-timeout", 5*time.Second, "per-network-check timeout")
	return cmd
}

func renderDoctor(cmd *cobra.Command, runtime command.Runtime, report apptunnel.DoctorReport, localTarget string) error {
	format, err := runtime.Format()
	if err != nil {
		return err
	}
	if format == output.FormatJSON {
		return output.PrintJSON(cmd.OutOrStdout(), report)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "Tunnel doctor")
	fmt.Fprintf(w, "control: reachable=%t\n", report.ControlReachable)
	if report.ServiceID != "" {
		fmt.Fprintf(w, "service: id=%s selected_allocation=%s selected_node=%s\n", report.ServiceID, displayValue(report.SelectedAllocation), displayValue(report.SelectedNodeID))
	}
	fmt.Fprintf(w, "session: id=%s allocation=%s status=%s bound=%s\n", displayValue(report.SessionID), displayValue(report.AllocationID), displayValue(report.Status), displayValue(report.BoundAddr))
	fmt.Fprintf(w, "relay: id=%s target=%s reachable=%t\n", displayValue(report.RelayID), displayValue(report.ClientTarget), report.RelayReachable)
	fmt.Fprintf(w, "node peer: target=%s state=%s\n", displayValue(report.NodeTarget), displayValue(report.NodePeer))
	fmt.Fprintf(w, "client peer: state=%s\n", displayValue(report.ClientPeer))
	if report.LocalReachable || strings.TrimSpace(localTarget) != "" {
		fmt.Fprintf(w, "local upstream: target=%s reachable=%t\n", displayValue(localTarget), report.LocalReachable)
	}
	if report.LastCloseReason != "" {
		fmt.Fprintf(w, "last close reason: %s\n", report.LastCloseReason)
	}
	if report.Recommendation != "" {
		fmt.Fprintf(w, "recommendation: %s\n", report.Recommendation)
	}
	for _, check := range report.Checks {
		fmt.Fprintf(w, "ok: %s\n", check)
	}
	for _, problem := range report.Problems {
		fmt.Fprintf(w, "problem: %s\n", problem)
	}
	for _, event := range report.RecentEvents {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	return nil
}

func probeRelay(ctx context.Context, target string, config apptunnel.RelayDialConfig, timeout time.Duration) bool {
	if strings.TrimSpace(target) == "" {
		return false
	}
	dialOptions, err := tunnelrelay.DialOptions(config)
	if err != nil {
		return false
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := grpcclient.NewReadyClient(dialCtx, target, dialOptions...)
	if err != nil {
		return false
	}
	defer conn.Close()
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(dialCtx)
	if err != nil {
		return false
	}
	_ = stream.CloseSend()
	return true
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
