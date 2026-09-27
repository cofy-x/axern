package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	nodetunnelv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/tunnel/v1"
	"github.com/cofy-x/axern/runtime/tunneld/internal/stdioframe"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type bridgeRelayStream struct {
	grpc.ClientStream
	ctx      context.Context
	fromNode chan *tunnelv1.TunnelFrame
	toNode   chan *tunnelv1.TunnelFrame
}

type rejectingTunnelClient struct {
	nodetunnelv1.AllocationTunnelClient
	err error
}

func (c rejectingTunnelClient) ValidateAllocationTunnel(context.Context, *nodetunnelv1.ValidateAllocationTunnelRequest, ...grpc.CallOption) (*nodetunnelv1.ValidateAllocationTunnelResponse, error) {
	return nil, c.err
}

type acceptingTunnelClient struct {
	nodetunnelv1.AllocationTunnelClient
	allocationID string
}

func (c acceptingTunnelClient) ValidateAllocationTunnel(_ context.Context, req *nodetunnelv1.ValidateAllocationTunnelRequest, _ ...grpc.CallOption) (*nodetunnelv1.ValidateAllocationTunnelResponse, error) {
	allocationID := c.allocationID
	if allocationID == "" {
		allocationID = req.GetAllocationID()
	}
	return &nodetunnelv1.ValidateAllocationTunnelResponse{AllocationID: allocationID}, nil
}

func TestStartValidatedRunscAgentRefusesLateTermination(t *testing.T) {
	// This gate runs after relay authentication and directly before runsc exec.
	// A stale watch item must not launch a guest listener after termination.
	d := &daemon{tunnel: rejectingTunnelClient{err: grpcstatus.Error(codes.FailedPrecondition, "Allocation terminated during relay handshake")}, runsc: runscConfig{binary: "/not/a/runsc/binary"}}
	agent, err := d.startValidatedRunscAgent(context.Background(), "alloc-test", 8765)
	if agent != nil || grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late termination: agent=%v, err=%v", agent, err)
	}
	if got := statusForSessionError(err); got != tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED {
		t.Fatalf("late termination status = %v, want FAILED", got)
	}
	if got := err.Error(); got != allocationValidationFailureReason {
		t.Fatalf("late termination reason = %q, want safe validation reason", got)
	}
}

func TestStartValidatedRunscAgentRetainsTransientValidationStatus(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Canceled} {
		t.Run(code.String(), func(t *testing.T) {
			d := &daemon{tunnel: rejectingTunnelClient{err: grpcstatus.Error(code, "node authorization temporarily unavailable")}, runsc: runscConfig{binary: "/not/a/runsc/binary"}}
			agent, err := d.startValidatedRunscAgent(context.Background(), "alloc-test", 8765)
			if agent != nil || grpcstatus.Code(err) != code {
				t.Fatalf("transient validation: agent=%v, err=%v", agent, err)
			}
			if got := statusForSessionError(err); got != tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED {
				t.Fatalf("transient validation status = %v, want DEGRADED", got)
			}
			if got := err.Error(); got != allocationValidationRetryReason {
				t.Fatalf("transient validation reason = %q, want safe retry reason", got)
			}
		})
	}
}

func TestStartValidatedRunscAgentRejectsIdentityMismatch(t *testing.T) {
	d := &daemon{tunnel: acceptingTunnelClient{allocationID: "different-allocation"}, runsc: runscConfig{binary: "/not/a/runsc/binary"}}
	agent, err := d.startValidatedRunscAgent(context.Background(), "alloc-test", 8765)
	if agent != nil || statusForSessionError(err) != tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED {
		t.Fatalf("identity mismatch: agent=%v, err=%v", agent, err)
	}
	if got := err.Error(); got != allocationValidationFailureReason || strings.Contains(got, "different-allocation") {
		t.Fatalf("identity mismatch reason = %q, want safe validation reason", got)
	}
}

func TestAgentProcessErrorClassifiesTypedLaunchFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status tunnelcontrolv1.TunnelSessionStatus
		reason string
	}{
		{"missing executable", os.ErrNotExist, tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED, agentStartupFailureReason},
		{"invalid executable", syscall.ENOEXEC, tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED, agentStartupFailureReason},
		{"process capacity", syscall.EAGAIN, tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED, agentStartupRetryReason},
		{"file descriptor limit", syscall.EMFILE, tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED, agentStartupRetryReason},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := agentProcessError(tc.err)
			if !errors.Is(err, tc.err) || statusForSessionError(err) != tc.status || err.Error() != tc.reason {
				t.Fatalf("agent process error = %v status=%v, want reason=%q status=%v", err, statusForSessionError(err), tc.reason, tc.status)
			}
		})
	}
}

func TestStartValidatedRunscAgentReportsSafePermanentStartupFailure(t *testing.T) {
	missingAgent := filepath.Join(t.TempDir(), "private-agent-binary")
	d := &daemon{tunnel: acceptingTunnelClient{}, runsc: runscConfig{binary: "/not/a/runsc/binary", agentBinary: missingAgent}}
	agent, err := d.startValidatedRunscAgent(context.Background(), "alloc-test", 8765)
	if agent != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing agent: agent=%v, err=%v", agent, err)
	}
	if got := statusForSessionError(err); got != tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED {
		t.Fatalf("agent startup status = %v, want FAILED", got)
	}
	if got := err.Error(); got != agentStartupFailureReason || strings.Contains(got, missingAgent) {
		t.Fatalf("agent startup reason = %q, want stable reason without local path", got)
	}
}

func TestStartValidatedRunscAgentReportsEarlyExitAsPermanentFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test requires Unix")
	}
	dir := t.TempDir()
	fakeRunsc := filepath.Join(dir, "runsc")
	if err := os.WriteFile(fakeRunsc, []byte("#!/bin/sh\nexit 42\n"), 0700); err != nil {
		t.Fatal(err)
	}
	agentBinary := filepath.Join(dir, "tunnel-agent")
	if err := os.WriteFile(agentBinary, []byte("test binary"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &daemon{tunnel: acceptingTunnelClient{}, runsc: runscConfig{binary: fakeRunsc, root: dir, agentBinary: agentBinary}}
	agent, err := d.startValidatedRunscAgent(context.Background(), "alloc-test", 8765)
	if agent != nil || err == nil {
		t.Fatalf("early exit: agent=%v, err=%v", agent, err)
	}
	if got := statusForSessionError(err); got != tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED {
		t.Fatalf("early exit status = %v, want FAILED", got)
	}
	if got := err.Error(); got != agentStartupFailureReason {
		t.Fatalf("early exit reason = %q, want %q", got, agentStartupFailureReason)
	}
}

func TestStartValidatedRunscAgentRejectsInvalidReadyMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test requires Unix")
	}
	dir := t.TempDir()
	fakeRunsc := filepath.Join(dir, "runsc")
	if err := os.WriteFile(fakeRunsc, []byte("#!/bin/sh\nprintf 'wrong!\\n'\nexec cat >/dev/null\n"), 0700); err != nil {
		t.Fatal(err)
	}
	agentBinary := filepath.Join(dir, "tunnel-agent")
	if err := os.WriteFile(agentBinary, []byte("test binary"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &daemon{tunnel: acceptingTunnelClient{}, runsc: runscConfig{binary: fakeRunsc, root: dir, agentBinary: agentBinary}}
	agent, err := d.startValidatedRunscAgent(context.Background(), "alloc-test", 8765)
	if agent != nil || !errors.Is(err, errInvalidAgentReady) {
		t.Fatalf("invalid ready: agent=%v, err=%v", agent, err)
	}
	if got := statusForSessionError(err); got != tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED {
		t.Fatalf("invalid ready status = %v, want FAILED", got)
	}
}

func (s *bridgeRelayStream) Send(frame *tunnelv1.TunnelFrame) error {
	select {
	case s.fromNode <- frame:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *bridgeRelayStream) Recv() (*tunnelv1.TunnelFrame, error) {
	select {
	case frame := <-s.toNode:
		return frame, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func TestBridgeAgentAndRelayKeepsPeerOpenOnNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guestOutput, hostInput := io.Pipe()
	guestInput, hostOutput := io.Pipe()
	defer guestOutput.Close()
	defer hostInput.Close()
	defer guestInput.Close()
	defer hostOutput.Close()
	agent := &runscAgent{stdout: guestOutput, stdin: hostOutput, wait: make(chan error)}
	relay := &bridgeRelayStream{ctx: ctx, fromNode: make(chan *tunnelv1.TunnelFrame, 1), toNode: make(chan *tunnelv1.TunnelFrame, 1)}
	done := make(chan error, 1)
	go func() { done <- bridgeAgentAndRelay(ctx, agent, relay) }()

	guest := stdioframe.New(guestInput, hostInput)
	if err := guest.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamOpen{StreamOpen: &tunnelv1.StreamOpen{StreamID: 17}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-relay.fromNode:
		if frame.GetStreamOpen().GetStreamID() != 17 {
			t.Fatalf("forwarded frame = %v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guest frame was not forwarded")
	}
	relay.toNode <- &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{StreamData: &tunnelv1.StreamData{StreamID: 17, Data: []byte("reply")}}}
	frame, err := guest.Recv()
	if err != nil || string(frame.GetStreamData().GetData()) != "reply" {
		t.Fatalf("guest frame = %v, err = %v", frame, err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || err != context.Canceled {
			t.Fatalf("bridge cancellation = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop after cancellation")
	}
}

func TestBridgeRejectsGuestPeerOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guestOutput, hostInput := io.Pipe()
	guestInput, hostOutput := io.Pipe()
	defer guestOutput.Close()
	defer hostInput.Close()
	defer guestInput.Close()
	defer hostOutput.Close()
	agent := &runscAgent{stdout: guestOutput, stdin: hostOutput, wait: make(chan error)}
	relay := &bridgeRelayStream{ctx: ctx, fromNode: make(chan *tunnelv1.TunnelFrame, 1), toNode: make(chan *tunnelv1.TunnelFrame, 1)}
	done := make(chan error, 1)
	go func() { done <- bridgeAgentAndRelay(ctx, agent, relay) }()
	guest := stdioframe.New(nil, hostInput)
	if err := guest.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{Token: "must-not-forward"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "peer_open") || strings.Contains(err.Error(), "must-not-forward") {
			t.Fatalf("peer_open rejection = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guest peer_open was not rejected")
	}
	select {
	case frame := <-relay.fromNode:
		t.Fatalf("guest peer_open reached relay: %v", frame)
	default:
	}
}

func TestBridgeRejectsRelayPeerOpenBeforeGuest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guestOutput, hostInput := io.Pipe()
	guestInput, hostOutput := io.Pipe()
	defer guestOutput.Close()
	defer hostInput.Close()
	defer guestInput.Close()
	defer hostOutput.Close()
	agent := &runscAgent{stdout: guestOutput, stdin: hostOutput, wait: make(chan error)}
	relay := &bridgeRelayStream{ctx: ctx, fromNode: make(chan *tunnelv1.TunnelFrame, 1), toNode: make(chan *tunnelv1.TunnelFrame, 1)}
	done := make(chan error, 1)
	go func() { done <- bridgeAgentAndRelay(ctx, agent, relay) }()
	relay.toNode <- &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{Token: "must-not-reach-guest"}}}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "peer_open") || strings.Contains(err.Error(), "must-not-reach-guest") {
			t.Fatalf("relay peer_open rejection = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay peer_open was not rejected")
	}
}

func TestStartRunscAgentUsesOwnedBidirectionalPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test requires Unix")
	}
	dir := t.TempDir()
	fakeRunsc := filepath.Join(dir, "runsc")
	if err := os.WriteFile(fakeRunsc, []byte("#!/bin/sh\nprintf 'ready\\n'\nexec cat >/dev/null\n"), 0700); err != nil {
		t.Fatal(err)
	}
	agentBinary := filepath.Join(dir, "tunnel-agent")
	if err := os.WriteFile(agentBinary, []byte("test binary"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &daemon{runsc: runscConfig{binary: fakeRunsc, root: dir, agentBinary: agentBinary}}
	ctx, cancel := context.WithCancel(context.Background())
	agent, err := d.startRunscAgent(ctx, "alloc-test", 8765)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	agent.stop()
	cancel()
	select {
	case <-agent.wait:
	case <-time.After(2 * time.Second):
		t.Fatal("runsc child was not reaped")
	}
}
