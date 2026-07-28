package relay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestForwardingDoesNotAcquireRegistryShardLock(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{}, WithMaxSessions(0))
	sessionID := "session-locked"
	client, node := testRegistryPair(t, server, sessionID)

	locked := server.registry.shard(sessionID)
	locked.mu.Lock()
	done := make(chan *peer, 1)
	go func() { done <- server.opposite(sessionID, client) }()
	select {
	case got := <-done:
		locked.mu.Unlock()
		if got != node {
			t.Fatalf("opposite peer = %#v, want node", got)
		}
	case <-time.After(time.Second):
		locked.mu.Unlock()
		t.Fatal("forwarding lookup acquired the session registry shard lock")
	}
}

func TestSessionRegistryMutatesDifferentShardsIndependently(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{}, WithMaxSessions(0))
	lockedID := "session-locked"
	otherID := sessionOnDifferentShard(server, lockedID)
	locked := server.registry.shard(lockedID)
	locked.mu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- server.register(otherID, newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT))
	}()
	select {
	case err := <-done:
		locked.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		locked.mu.Unlock()
		t.Fatal("session mutation blocked behind an unrelated shard")
	}
}

func TestSessionRegistryConcurrentRegistrationEnforcesGlobalLimit(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{}, WithMaxSessions(7))
	var successes atomic.Int64
	var wg sync.WaitGroup
	for i := range 128 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := server.register(fmt.Sprintf("session-%d", i), newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT))
			if err == nil {
				successes.Add(1)
				return
			}
			if grpcstatus.Code(err) != codes.ResourceExhausted {
				t.Errorf("register error = %v, want ResourceExhausted", err)
			}
		}(i)
	}
	wg.Wait()
	if got := successes.Load(); got != 7 {
		t.Fatalf("successful registrations = %d, want 7", got)
	}
	if got := server.registry.sessions.Load(); got != 7 {
		t.Fatalf("active sessions = %d, want 7", got)
	}
	if got := server.registry.clientPeer.Load(); got != 7 {
		t.Fatalf("active client peers = %d, want 7", got)
	}
}

func TestWaitOppositeWakesOnPairChange(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{}, WithPairWaitTimeout(time.Second))
	client := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)
	if err := server.register("session-wait", client); err != nil {
		t.Fatal(err)
	}
	_, changed := server.oppositeState("session-wait", client)
	done := make(chan *peer, 1)
	go func() { done <- server.waitOpposite(context.Background(), "session-wait", client) }()
	node := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)
	if err := server.register("session-wait", node); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("pair change notification was not closed")
	}
	select {
	case got := <-done:
		if got != node {
			t.Fatalf("wait returned %#v, want node", got)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter was not woken by pair registration")
	}
}

func TestWaitOppositeStopsWhenPeerCloses(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{}, WithPairWaitTimeout(time.Second))
	client := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)
	if err := server.register("session-close", client); err != nil {
		t.Fatal(err)
	}
	done := make(chan *peer, 1)
	go func() { done <- server.waitOpposite(context.Background(), "session-close", client) }()
	client.close()
	server.unregister("session-close", client)
	select {
	case got := <-done:
		if got != nil {
			t.Fatalf("wait returned %#v, want nil", got)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not stop after peer unregister")
	}
}

func TestReplacingPeerRejectsStaleForwardingAndUnregister(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{})
	oldClient, _ := testRegistryPair(t, server, "session-replace")
	newClient := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)
	if err := server.register("session-replace", newClient); err != nil {
		t.Fatal(err)
	}
	newNode := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)
	if err := server.register("session-replace", newNode); err != nil {
		t.Fatal(err)
	}
	if got := server.opposite("session-replace", oldClient); got != nil {
		t.Fatalf("stale client resolved opposite peer %#v", got)
	}
	server.unregister("session-replace", oldClient)
	if got := server.opposite("session-replace", newClient); got != newNode {
		t.Fatalf("stale unregister removed replacement pair: got %#v", got)
	}
}

func TestReplacedPeerCannotCloseReplacementSession(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{})
	oldClient, _ := testRegistryPair(t, server, "session-replace-close")
	newClient := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)
	if err := server.register("session-replace-close", newClient); err != nil {
		t.Fatal(err)
	}
	newNode := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)
	if err := server.register("session-replace-close", newNode); err != nil {
		t.Fatal(err)
	}

	server.closePeerSessionWithError("session-replace-close", oldClient, grpcstatus.Error(codes.Unavailable, "stale peer failure"))
	if got := server.opposite("session-replace-close", newClient); got != newNode {
		t.Fatalf("stale peer closed replacement pair: got %#v", got)
	}
	select {
	case <-newClient.done:
		t.Fatal("replacement client was closed by stale peer")
	case <-newNode.done:
		t.Fatal("replacement node was closed by stale peer")
	default:
	}
}

func TestRegisterRejectsInvalidPeerKindWithoutLeakingSession(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{})
	err := server.register("session-invalid", newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_UNSPECIFIED))
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("register error = %v, want InvalidArgument", err)
	}
	if got := server.registry.sessions.Load(); got != 0 {
		t.Fatalf("active sessions = %d, want 0", got)
	}
}

func TestQueueFullClosesPairWithSingleTerminalCause(t *testing.T) {
	t.Parallel()
	server := New(fakeControl{})
	client, node := testRegistryPair(t, server, "session-full")
	node.send <- &tunnelv1.TunnelFrame{}
	wantErr := grpcstatus.Error(codes.ResourceExhausted, "tunnel peer send queue full")
	err := server.enqueue(context.Background(), node, &tunnelv1.TunnelFrame{})
	if grpcstatus.Code(err) != codes.ResourceExhausted {
		t.Fatalf("enqueue error = %v, want ResourceExhausted", err)
	}
	for name, current := range map[string]*peer{"client": client, "node": node} {
		select {
		case <-current.done:
		case <-time.After(time.Second):
			t.Fatalf("%s peer did not close", name)
		}
		if !errors.Is(current.error(), wantErr) && grpcstatus.Code(current.error()) != codes.ResourceExhausted {
			t.Fatalf("%s terminal error = %v, want queue full", name, current.error())
		}
	}
	if got := server.registry.sessions.Load(); got != 0 {
		t.Fatalf("active sessions after queue full = %d, want 0", got)
	}
	if got := server.registry.clientPeer.Load() + server.registry.nodePeer.Load(); got != 0 {
		t.Fatalf("active peers after queue full = %d, want 0", got)
	}
}

func TestPeerKeepsFirstTerminalCause(t *testing.T) {
	t.Parallel()
	p := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)
	first := grpcstatus.Error(codes.PermissionDenied, "revoked")
	p.closeWithError(first)
	p.closeWithError(context.Canceled)
	if !errors.Is(p.error(), first) && grpcstatus.Code(p.error()) != codes.PermissionDenied {
		t.Fatalf("terminal error = %v, want first cause", p.error())
	}
}

func testRegistryPair(t *testing.T, server *Server, sessionID string) (*peer, *peer) {
	t.Helper()
	client := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)
	node := newRegistryPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)
	if err := server.register(sessionID, client); err != nil {
		t.Fatal(err)
	}
	if err := server.register(sessionID, node); err != nil {
		t.Fatal(err)
	}
	return client, node
}

func newRegistryPeer(kind tunnelcontrolv1.TunnelPeerKind) *peer {
	return &peer{
		kind: kind,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}
}

func sessionOnDifferentShard(server *Server, sessionID string) string {
	wantDifferentFrom := server.registry.shard(sessionID)
	for i := 0; ; i++ {
		candidate := fmt.Sprintf("session-other-%d", i)
		if server.registry.shard(candidate) != wantDifferentFrom {
			return candidate
		}
	}
}
