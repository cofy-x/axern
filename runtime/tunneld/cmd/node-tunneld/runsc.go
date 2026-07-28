package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
)

func (d *daemon) serveRunscSession(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token string, _ *nodeoperatorv1.ResolveSandboxNetworkResponse) error {
	edgeTarget := session.GetNodeEdgeTarget()
	if edgeTarget == "" {
		return fmt.Errorf("session node_edge_target is required")
	}
	sandboxEdgeTarget, err := resolveSandboxReachableTarget(edgeTarget)
	if err != nil {
		return degradedSessionError(err)
	}
	relayServerName, err := serverNameFromTarget(edgeTarget)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(session.GetRemotePort())))
	fmt.Fprintf(os.Stderr, "node-tunneld: starting runsc tunnel agent session=%s edge=%s sandbox_edge=%s listen=%s\n", session.GetSessionID(), edgeTarget, sandboxEdgeTarget, addr)
	waitCh, err := d.startRunscAgent(ctx, session, token, sandboxEdgeTarget, relayServerName)
	if err != nil {
		return err
	}
	_, _ = d.node.ReportTunnelSessionStatus(ctx, &nodev1.ReportTunnelSessionStatusRequest{
		NodeID:        d.nodeID,
		NodeAuthToken: d.nodeAuthToken,
		SessionID:     session.GetSessionID(),
		Status:        tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
		BoundAddr:     addr,
	})
	select {
	case <-ctx.Done():
		return nil
	case err := <-waitCh:
		if err == nil {
			return degradedSessionError(fmt.Errorf("runsc tunnel agent exited"))
		}
		return degradedSessionError(fmt.Errorf("runsc tunnel agent exited: %w", err))
	}
}

func (d *daemon) startRunscAgent(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token, edgeTarget, relayServerName string) (<-chan error, error) {
	agent, err := os.Open(d.runsc.agentBinary)
	if err != nil {
		return nil, err
	}
	defer agent.Close()
	args := []string{"--root", d.runsc.root}
	if d.runsc.ignoreCgroups {
		args = append(args, "--ignore-cgroups")
	}
	args = append(args,
		"exec",
		"-exec-fd", "3",
		session.GetAllocationID(),
		"/proc/self/exe",
		"-session-id", session.GetSessionID(),
		"-token", token,
		"-edge-target", edgeTarget,
		"-listen-port", strconv.Itoa(int(session.GetRemotePort())),
	)
	if d.relay.caCert != "" {
		caPEM, err := os.ReadFile(d.relay.caCert)
		if err != nil {
			return nil, err
		}
		args = append(args, "-relay-tls-ca-pem", string(caPEM))
		if relayServerName != "" {
			args = append(args, "-relay-server-name", relayServerName)
		}
	}
	cmd := exec.CommandContext(ctx, d.runsc.binary, args...)
	cmd.ExtraFiles = []*os.File{agent}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	readyCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "ready" {
				readyCh <- nil
				return
			}
		}
		if err := scanner.Err(); err != nil {
			readyCh <- err
			return
		}
		readyCh <- io.ErrUnexpectedEOF
	}()
	select {
	case err := <-readyCh:
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "node-tunneld: runsc tunnel agent ready session=%s\n", session.GetSessionID())
		return waitCh, nil
	case err := <-waitCh:
		if err == nil {
			return nil, fmt.Errorf("runsc tunnel agent exited before ready")
		}
		return nil, err
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("runsc tunnel agent did not become ready")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
