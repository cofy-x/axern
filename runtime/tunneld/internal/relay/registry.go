package relay

import (
	"hash/maphash"
	"sync"
	"sync/atomic"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const maximumSessionRegistryShards = 32

type sessionRegistry struct {
	seed       maphash.Seed
	shards     []sessionShard
	capacity   sync.Mutex
	sessions   atomic.Int64
	clientPeer atomic.Int64
	nodePeer   atomic.Int64
}

type sessionShard struct {
	mu    sync.Mutex
	pairs map[string]*pair
}

func (r *sessionRegistry) init(seed maphash.Seed, maxSessions int) {
	shardCount := maximumSessionRegistryShards
	if maxSessions > 0 && maxSessions < shardCount {
		shardCount = maxSessions
	}
	r.seed = seed
	r.shards = make([]sessionShard, shardCount)
	for i := range r.shards {
		r.shards[i].pairs = make(map[string]*pair)
	}
}

func (r *sessionRegistry) shard(sessionID string) *sessionShard {
	index := maphash.String(r.seed, sessionID) % uint64(len(r.shards))
	return &r.shards[index]
}

func (s *Server) register(sessionID string, p *peer) error {
	if p.kind != tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT && p.kind != tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE {
		return grpcstatus.Error(codes.InvalidArgument, "tunnel peer kind must be client or node")
	}
	r := &s.registry
	shard := r.shard(sessionID)
	shard.mu.Lock()
	slot := shard.pairs[sessionID]
	if slot != nil {
		registerPeer(r, slot, sessionID, p)
		shard.mu.Unlock()
		return nil
	}
	shard.mu.Unlock()

	// Only creation needs global coordination so the process-wide limit stays
	// strict while existing sessions continue mutating independently by shard.
	r.capacity.Lock()
	defer r.capacity.Unlock()
	shard.mu.Lock()
	defer shard.mu.Unlock()
	slot = shard.pairs[sessionID]
	if slot == nil {
		if s.maxSessions > 0 && r.sessions.Load() >= int64(s.maxSessions) {
			return grpcstatus.Error(codes.ResourceExhausted, "tunnel relay active session limit reached")
		}
		slot = &pair{changed: make(chan struct{})}
		shard.pairs[sessionID] = slot
		r.sessions.Add(1)
	}
	registerPeer(r, slot, sessionID, p)
	return nil
}

func registerPeer(r *sessionRegistry, slot *pair, sessionID string, p *peer) {
	p.sessionID = sessionID
	p.pair = slot
	switch p.kind {
	case tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT:
		if current := slot.client.Load(); current != nil {
			current.close()
			if opposite := slot.node.Swap(nil); opposite != nil {
				opposite.close()
				r.nodePeer.Add(-1)
			}
		} else {
			r.clientPeer.Add(1)
		}
		slot.client.Store(p)
	case tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE:
		if current := slot.node.Load(); current != nil {
			current.close()
			if opposite := slot.client.Swap(nil); opposite != nil {
				opposite.close()
				r.clientPeer.Add(-1)
			}
		} else {
			r.nodePeer.Add(1)
		}
		slot.node.Store(p)
	}
	slot.notify()
}

func (s *Server) unregister(sessionID string, p *peer) {
	r := &s.registry
	shard := r.shard(sessionID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	slot := shard.pairs[sessionID]
	if slot == nil {
		return
	}
	changed := false
	if slot.client.Load() == p {
		if opposite := slot.node.Swap(nil); opposite != nil {
			opposite.close()
			r.nodePeer.Add(-1)
		}
		slot.client.Store(nil)
		r.clientPeer.Add(-1)
		changed = true
	}
	if slot.node.Load() == p {
		if opposite := slot.client.Swap(nil); opposite != nil {
			opposite.close()
			r.clientPeer.Add(-1)
		}
		slot.node.Store(nil)
		r.nodePeer.Add(-1)
		changed = true
	}
	if !changed {
		return
	}
	slot.notify()
	if slot.client.Load() == nil && slot.node.Load() == nil {
		delete(shard.pairs, sessionID)
		r.sessions.Add(-1)
	}
}

func (s *Server) closePeerSessionWithError(sessionID string, source *peer, err error) {
	r := &s.registry
	shard := r.shard(sessionID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	slot := shard.pairs[sessionID]
	if slot == nil {
		return
	}
	// A superseded peer must never terminate the replacement generation that
	// now owns the same public session ID.
	if slot.client.Load() != source && slot.node.Load() != source {
		return
	}
	if current := slot.client.Swap(nil); current != nil {
		current.closeWithError(err)
		r.clientPeer.Add(-1)
	}
	if current := slot.node.Swap(nil); current != nil {
		current.closeWithError(err)
		r.nodePeer.Add(-1)
	}
	slot.notify()
	delete(shard.pairs, sessionID)
	r.sessions.Add(-1)
}

func (s *Server) opposite(sessionID string, p *peer) *peer {
	if p == nil || p.sessionID != sessionID || p.pair == nil {
		return nil
	}
	if p.kind == tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT {
		if p.pair.client.Load() != p {
			return nil
		}
		return p.pair.node.Load()
	}
	if p.pair.node.Load() != p {
		return nil
	}
	return p.pair.client.Load()
}

func (s *Server) oppositeState(sessionID string, p *peer) (*peer, <-chan struct{}) {
	if p == nil || p.sessionID != sessionID || p.pair == nil {
		return nil, nil
	}
	slot := p.pair
	slot.signalMu.Lock()
	defer slot.signalMu.Unlock()
	if p.kind == tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT {
		if slot.client.Load() != p {
			return nil, slot.changed
		}
		return slot.node.Load(), slot.changed
	}
	if slot.node.Load() != p {
		return nil, slot.changed
	}
	return slot.client.Load(), slot.changed
}

func (p *pair) notify() {
	p.signalMu.Lock()
	defer p.signalMu.Unlock()
	close(p.changed)
	p.changed = make(chan struct{})
}
