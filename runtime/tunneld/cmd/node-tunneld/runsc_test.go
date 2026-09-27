package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	nodetunnelv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/tunnel/v1"
	"github.com/cofy-x/axern/runtime/tunneld/internal/stdioframe"
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
}

func (rejectingTunnelClient) ValidateAllocationTunnel(context.Context, *nodetunnelv1.ValidateAllocationTunnelRequest, ...grpc.CallOption) (*nodetunnelv1.ValidateAllocationTunnelResponse, error) {
	return nil, grpcstatus.Error(codes.FailedPrecondition, "Allocation terminated during relay handshake")
}

func TestStartValidatedRunscAgentRefusesLateTermination(t *testing.T) {
	// This gate runs after relay authentication and directly before runsc exec.
	// A stale watch item must not launch a guest listener after termination.
	d := &daemon{tunnel: rejectingTunnelClient{}, runsc: runscConfig{binary: "/not/a/runsc/binary"}}
	agent, err := d.startValidatedRunscAgent(context.Background(), "alloc-test", 8765)
	if agent != nil || grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late termination: agent=%v, err=%v", agent, err)
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
