package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/runtime/tunneld/internal/relaytls"
	"github.com/cofy-x/axern/runtime/tunneld/internal/stdioframe"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const agentReadyMessage = stdioframe.ReadyLine

const (
	agentStartupFailureReason = "sandbox tunnel agent failed before readiness"
	agentStartupRetryReason   = "sandbox tunnel agent temporarily unavailable"
)

var errInvalidAgentReady = errors.New("runsc tunnel agent returned an invalid ready message")

type runscAgent struct {
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	process *os.Process
	wait    <-chan error
}

func (a *runscAgent) stop() {
	_ = a.stdin.Close()
	_ = a.stdout.Close()
	_ = a.process.Kill()
}

func (d *daemon) serveRunscSession(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token, nodeEdgeTarget string) error {
	edgeTarget := strings.TrimSpace(nodeEdgeTarget)
	if edgeTarget == "" {
		return fmt.Errorf("node tunnel relay target is required")
	}
	if session.GetRemotePort() <= 0 || session.GetRemotePort() > 65535 {
		return fmt.Errorf("tunnel remote port is invalid")
	}
	relayServerName, err := serverNameFromTarget(edgeTarget)
	if err != nil {
		return err
	}
	dialOpts, err := relaytls.DialOptions(relaytls.ClientConfig{CACert: d.relay.caCert, ServerName: relayServerName})
	if err != nil {
		return err
	}
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	dialCtx, stopDial := context.WithTimeout(bridgeCtx, 10*time.Second)
	conn, err := grpcclient.NewReadyClient(dialCtx, edgeTarget, dialOpts...)
	stopDial()
	if err != nil {
		return degradedSessionError(err)
	}
	defer conn.Close()
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(bridgeCtx)
	if err != nil {
		return degradedSessionError(err)
	}
	if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: session.GetSessionID(),
		PeerKind:  tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE,
		Token:     token,
	}}}); err != nil {
		return degradedSessionError(err)
	}
	// Relay transport readiness is not authorization readiness. Prove that the
	// relay accepted this exact session and node token before exposing ready.
	const readinessPing = "node-tunnel-ready"
	readyTimeout := time.AfterFunc(10*time.Second, cancel)
	if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Ping{Ping: &tunnelv1.Ping{ID: readinessPing}}}); err != nil {
		readyTimeout.Stop()
		return degradedSessionError(err)
	}
	for {
		frame, err := stream.Recv()
		if err != nil {
			readyTimeout.Stop()
			if grpcstatus.Code(err) == codes.PermissionDenied || grpcstatus.Code(err) == codes.Unauthenticated {
				return err
			}
			return degradedSessionError(err)
		}
		if frame.GetPong().GetID() == readinessPing {
			break
		}
		if ping := frame.GetPing(); ping != nil {
			if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Pong{Pong: &tunnelv1.Pong{ID: ping.GetID()}}}); err != nil {
				readyTimeout.Stop()
				return degradedSessionError(err)
			}
			continue
		}
		readyTimeout.Stop()
		return fmt.Errorf("relay sent an unexpected frame before tunnel readiness")
	}
	readyTimeout.Stop()

	// Relay authentication can take several seconds. Recheck the local durable
	// binding and live lease immediately before entering the sandbox so a
	// terminal or expired Allocation is not started from a stale watch item.
	agent, err := d.startValidatedRunscAgent(bridgeCtx, session.GetAllocationID(), session.GetRemotePort())
	if err != nil {
		return err
	}
	defer agent.stop()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(session.GetRemotePort())))
	reportCtx, stopReport := context.WithTimeout(bridgeCtx, 5*time.Second)
	_, err = d.node.ReportTunnelSessionStatus(reportCtx, &nodev1.ReportTunnelSessionStatusRequest{
		NodeID:    d.nodeID,
		SessionID: session.GetSessionID(),
		Status:    tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
		BoundAddr: addr,
	})
	stopReport()
	if err != nil {
		return degradedSessionError(fmt.Errorf("report tunnel readiness: %w", err))
	}
	if err := bridgeAgentAndRelay(bridgeCtx, agent, stream); err != nil && ctx.Err() == nil {
		return degradedSessionError(fmt.Errorf("tunnel bridge closed: %w", err))
	}
	return nil
}

func (d *daemon) startValidatedRunscAgent(ctx context.Context, allocationID string, port int32) (*runscAgent, error) {
	if err := d.validateAllocationTunnel(ctx, allocationID); err != nil {
		return nil, err
	}
	return d.startRunscAgent(ctx, allocationID, port)
}

func agentProcessError(err error) error {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, exec.ErrDot) || errors.Is(err, syscall.ENOEXEC) {
		return failedSessionError(err, agentStartupFailureReason)
	}
	return agentRetryError(err)
}

func agentRetryError(err error) error {
	return sessionStatusError{status: tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED, err: err, reason: agentStartupRetryReason}
}

func agentFailureError(err error) error {
	return failedSessionError(err, agentStartupFailureReason)
}

func bridgeAgentAndRelay(ctx context.Context, agent *runscAgent, stream tunnelv1.TunnelRelay_ConnectPeerClient) error {
	peer := stdioframe.New(agent.stdout, agent.stdin)
	errs := make(chan error, 2)
	go func() {
		for {
			frame, err := peer.Recv()
			if err != nil {
				errs <- err
				return
			}
			if frame.GetPeerOpen() != nil {
				errs <- fmt.Errorf("sandbox tunnel agent sent a peer_open frame")
				return
			}
			if err := stream.Send(frame); err != nil {
				errs <- err
				return
			}
		}
	}()
	go func() {
		for {
			frame, err := stream.Recv()
			if err != nil {
				errs <- err
				return
			}
			if frame.GetPeerOpen() != nil {
				errs <- fmt.Errorf("relay sent a peer_open frame after tunnel authentication")
				return
			}
			if err := peer.Send(frame); err != nil {
				errs <- err
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-agent.wait:
		if err == nil {
			return fmt.Errorf("runsc tunnel agent exited")
		}
		return fmt.Errorf("runsc tunnel agent exited: %w", err)
	case err := <-errs:
		return err
	}
}

func (d *daemon) startRunscAgent(ctx context.Context, allocationID string, port int32) (*runscAgent, error) {
	file, err := os.Open(d.runsc.agentBinary)
	if err != nil {
		return nil, agentProcessError(err)
	}
	defer file.Close()
	args := []string{"--root", d.runsc.root}
	if d.runsc.ignoreCgroups {
		args = append(args, "--ignore-cgroups")
	}
	args = append(args, "exec", "-exec-fd", "3", allocationID, "/proc/self/exe", "-listen-port", strconv.Itoa(int(port)))
	cmd := exec.CommandContext(ctx, d.runsc.binary, args...)
	cmd.ExtraFiles = []*os.File{file}
	cmd.Stderr = os.Stderr
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		return nil, agentRetryError(err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdinReader.Close()
		_ = stdinWriter.Close()
		return nil, agentRetryError(err)
	}
	// Own both pipe ends explicitly. StdoutPipe/StdinPipe must not be used with
	// a concurrent cmd.Wait: Wait is allowed to close those pipes before the
	// frame reader has consumed the last bytes.
	cmd.Stdin = stdinReader
	cmd.Stdout = stdoutWriter
	if err := cmd.Start(); err != nil {
		_ = stdinReader.Close()
		_ = stdinWriter.Close()
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return nil, agentProcessError(err)
	}
	_ = stdinReader.Close()
	_ = stdoutWriter.Close()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	agent := &runscAgent{stdin: stdinWriter, stdout: stdoutReader, process: cmd.Process, wait: wait}
	ready := make(chan error, 1)
	go func() {
		buf := make([]byte, len(agentReadyMessage))
		if _, err := io.ReadFull(stdoutReader, buf); err != nil {
			ready <- err
			return
		}
		if string(buf) != agentReadyMessage {
			ready <- errInvalidAgentReady
			return
		}
		ready <- nil
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err := <-ready:
		if err != nil {
			agent.stop()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, errInvalidAgentReady) {
				return nil, agentFailureError(err)
			}
			return nil, agentRetryError(err)
		}
		return agent, nil
	case err := <-wait:
		agent.stop()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			return nil, agentFailureError(fmt.Errorf("runsc tunnel agent exited before ready"))
		}
		return nil, agentFailureError(err)
	case <-timer.C:
		agent.stop()
		return nil, agentRetryError(fmt.Errorf("runsc tunnel agent did not become ready"))
	case <-ctx.Done():
		agent.stop()
		return nil, ctx.Err()
	}
}
