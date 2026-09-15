package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"io"
	"strings"
	"sync"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

const accessGrantWatchReconnectDelay = time.Second

type AccessGrantCache struct {
	mu      sync.RWMutex
	byToken map[string]*nodev1.NodeAllocationAccessGrant
	byGrant map[string]string
	changed chan struct{}
}

func NewAccessGrantCache() *AccessGrantCache {
	return &AccessGrantCache{
		byToken: make(map[string]*nodev1.NodeAllocationAccessGrant),
		byGrant: make(map[string]string),
		changed: make(chan struct{}),
	}
}

func (c *AccessGrantCache) Apply(grants []*nodev1.NodeAllocationAccessGrant) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	for tokenHash, grant := range c.byToken {
		if grant == nil || grant.GetExpiresAt() == nil || !grant.GetExpiresAt().AsTime().After(now) {
			delete(c.byToken, tokenHash)
			if grantID := strings.TrimSpace(grant.GetGrantID()); grantID != "" && c.byGrant[grantID] == tokenHash {
				delete(c.byGrant, grantID)
			}
		}
	}
	applied := false
	for _, grant := range grants {
		if grant == nil || strings.TrimSpace(grant.GetAllocationID()) == "" || grant.GetRevision() <= 0 {
			continue
		}
		tokenHash := strings.ToLower(strings.TrimSpace(grant.GetValidationTokenHash()))
		if tokenHash == "" {
			continue
		}
		grantID := strings.TrimSpace(grant.GetGrantID())
		if previous := c.byToken[c.byGrant[grantID]]; previous != nil && previous.GetRevision() >= grant.GetRevision() {
			continue
		}
		if previous := c.byGrant[grantID]; grantID != "" && previous != "" && previous != tokenHash {
			delete(c.byToken, previous)
		}
		c.byToken[tokenHash] = cloneAccessGrant(grant)
		if grantID != "" {
			c.byGrant[grantID] = tokenHash
		}
		applied = true
	}
	if applied {
		close(c.changed)
		c.changed = make(chan struct{})
	}
}

func (c *AccessGrantCache) Validate(allocationID string, token string, now time.Time) bool {
	valid, _ := c.validationState(allocationID, token, gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, now)
	return valid
}

func (c *AccessGrantCache) validationState(allocationID string, token string, purpose gatewayv1.AllocationAccessPurpose, now time.Time) (valid, known bool) {
	if c == nil || strings.TrimSpace(allocationID) == "" || strings.TrimSpace(token) == "" {
		return false, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	hash := accessGrantTokenHash(token)
	grant, known := c.byToken[hash]
	if !known || grant == nil || grant.GetAllocationID() != strings.TrimSpace(allocationID) {
		return false, false
	}
	return grant.GetPurpose() == purpose && !grant.GetRevoked() && grant.GetExpiresAt() != nil && grant.GetExpiresAt().AsTime().After(now), true
}

func (c *AccessGrantCache) WaitValidate(ctx context.Context, allocationID string, token string, purpose gatewayv1.AllocationAccessPurpose, now func() time.Time) (bool, bool) {
	if c == nil || now == nil {
		return false, false
	}
	waited := false
	for {
		valid, known := c.validationState(allocationID, token, purpose, now())
		if valid || known {
			return valid, waited
		}
		c.mu.RLock()
		changed := c.changed
		c.mu.RUnlock()
		valid, known = c.validationState(allocationID, token, purpose, now())
		if valid || known {
			return valid, waited
		}
		waited = true
		select {
		case <-ctx.Done():
			return false, waited
		case <-changed:
		}
	}
}

type AccessGrantWatcher struct {
	target  string
	nodeID  string
	cache   *AccessGrantCache
	control NodeControlClientProvider
	ctx     context.Context
	cancel  context.CancelFunc

	stopCh    chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
	wg        sync.WaitGroup
}

type AccessGrantWatcherOption func(*AccessGrantWatcher)

func WithAccessGrantWatcherTarget(target string) AccessGrantWatcherOption {
	return func(w *AccessGrantWatcher) {
		w.target = strings.TrimSpace(target)
	}
}

func WithAccessGrantWatcherNode(nodeID string) AccessGrantWatcherOption {
	return func(w *AccessGrantWatcher) {
		w.nodeID = strings.TrimSpace(nodeID)
	}
}

func WithAccessGrantWatcherControl(control NodeControlClientProvider) AccessGrantWatcherOption {
	return func(w *AccessGrantWatcher) { w.control = control }
}

func WithAccessGrantWatcherCache(cache *AccessGrantCache) AccessGrantWatcherOption {
	return func(w *AccessGrantWatcher) {
		w.cache = cache
	}
}

func NewAccessGrantWatcher(options ...AccessGrantWatcherOption) *AccessGrantWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	w := &AccessGrantWatcher{
		stopCh: make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
	for _, option := range options {
		if option != nil {
			option(w)
		}
	}
	if w.target == "" || w.nodeID == "" || w.control == nil || w.cache == nil {
		cancel()
		return nil
	}
	return w
}

func (w *AccessGrantWatcher) Start() {
	if w == nil {
		return
	}
	w.startOnce.Do(func() {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			var revision int64
			for {
				next, err := w.watchOnce(revision)
				if err != nil {
					if w.ctx.Err() != nil {
						return
					}
					logrus.WithError(err).Warn("control-plane allocation access grant watch failed")
				}
				if next > revision {
					revision = next
				}
				select {
				case <-time.After(accessGrantWatchReconnectDelay):
				case <-w.stopCh:
					return
				}
			}
		}()
	})
}

func (w *AccessGrantWatcher) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		w.cancel()
		close(w.stopCh)
		w.wg.Wait()
		if w.control != nil {
			if err := w.control.Close(); err != nil {
				logrus.WithError(err).Warn("close control-plane allocation access grant watcher client")
			}
		}
	})
}

func (w *AccessGrantWatcher) watchOnce(afterRevision int64) (int64, error) {
	client, err := w.control.Client(w.ctx)
	if err != nil {
		return afterRevision, err
	}
	stream, err := client.WatchAllocationAccessGrants(w.ctx, &nodev1.WatchAllocationAccessGrantsRequest{
		NodeID:        w.nodeID,
		AfterRevision: afterRevision,
	})
	if err != nil {
		return afterRevision, err
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return afterRevision, nil
			}
			return afterRevision, err
		}
		w.cache.Apply(resp.GetGrants())
		if resp.GetCurrentRevision() > afterRevision {
			afterRevision = resp.GetCurrentRevision()
		}
	}
}

func accessGrantTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func cloneAccessGrant(in *nodev1.NodeAllocationAccessGrant) *nodev1.NodeAllocationAccessGrant {
	if in == nil {
		return nil
	}
	cloned, ok := proto.Clone(in).(*nodev1.NodeAllocationAccessGrant)
	if !ok {
		return nil
	}
	return cloned
}
