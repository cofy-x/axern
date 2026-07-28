package relay

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

func BenchmarkRelayOppositeSingleSessionParallel(b *testing.B) {
	server := New(fakeControl{}, WithMaxSessions(0))
	client, node := benchmarkPair(b, server, "session-single")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if server.opposite("session-single", client) != node {
				b.Fatal("opposite peer changed")
			}
		}
	})
}

func BenchmarkRelayOppositeManySessionsParallel(b *testing.B) {
	server := New(fakeControl{}, WithMaxSessions(0))
	const sessions = 1024
	sessionIDs := make([]string, sessions)
	clients := make([]*peer, sessions)
	nodes := make([]*peer, sessions)
	for i := range sessions {
		sessionIDs[i] = fmt.Sprintf("session-%d", i)
		clients[i], nodes[i] = benchmarkPair(b, server, sessionIDs[i])
	}
	var sequence atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			index := int(sequence.Add(1) % sessions)
			if server.opposite(sessionIDs[index], clients[index]) != nodes[index] {
				b.Fatal("opposite peer changed")
			}
		}
	})
}

func BenchmarkRelayPairWaitWakeup(b *testing.B) {
	server := New(fakeControl{}, WithMaxSessions(0), WithPairWaitTimeout(time.Second))
	for i := range b.N {
		sessionID := fmt.Sprintf("session-wait-%d", i)
		client := benchmarkPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)
		if err := server.register(sessionID, client); err != nil {
			b.Fatal(err)
		}
		done := make(chan *peer, 1)
		go func() {
			done <- server.waitOpposite(context.Background(), sessionID, client)
		}()
		time.Sleep(time.Millisecond)
		node := benchmarkPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)
		if err := server.register(sessionID, node); err != nil {
			b.Fatal(err)
		}
		if got := <-done; got != node {
			b.Fatal("wait returned wrong opposite peer")
		}
	}
}

func benchmarkPair(b *testing.B, server *Server, sessionID string) (*peer, *peer) {
	b.Helper()
	client := benchmarkPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)
	node := benchmarkPeer(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)
	if err := server.register(sessionID, client); err != nil {
		b.Fatal(err)
	}
	if err := server.register(sessionID, node); err != nil {
		b.Fatal(err)
	}
	return client, node
}

func benchmarkPeer(kind tunnelcontrolv1.TunnelPeerKind) *peer {
	return &peer{
		kind: kind,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}
}
