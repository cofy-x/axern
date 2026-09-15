package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		if grant == nil || strings.TrimSpace(grant.GetAllocationID()) == "" {
			continue
		}
		tokenHash := strings.ToLower(strings.TrimSpace(grant.GetValidationTokenHash()))
		if tokenHash == "" {
			continue
		}
		grantID := strings.TrimSpace(grant.GetGrantID())
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
	valid, _ := c.validationState(allocationID, token, now)
	return valid
}

func (c *AccessGrantCache) validationState(allocationID string, token string, now time.Time) (valid, known bool) {
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
	return !grant.GetRevoked() && grant.GetExpiresAt() != nil && grant.GetExpiresAt().AsTime().After(now), true
}

func (c *AccessGrantCache) WaitValidate(ctx context.Context, allocationID string, token string, now func() time.Time) (bool, bool) {
	if c == nil || now == nil {
		return false, false
	}
	waited := false
	for {
		valid, known := c.validationState(allocationID, token, now())
		if valid || known {
			return valid, waited
		}
		c.mu.RLock()
		changed := c.changed
		c.mu.RUnlock()
		valid, known = c.validationState(allocationID, token, now())
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
	target         string
	nodeID         string
	nodeCredential string
	cache          *AccessGrantCache
	control        NodeControlClientProvider
	tlsCACert      string
	tlsCert        string
	tlsKey         string
	ctx            context.Context
	cancel         context.CancelFunc

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

func WithAccessGrantWatcherNode(nodeID, nodeCredential string) AccessGrantWatcherOption {
	return func(w *AccessGrantWatcher) {
		w.nodeID = strings.TrimSpace(nodeID)
		w.nodeCredential = strings.TrimSpace(nodeCredential)
	}
}

func WithAccessGrantWatcherTLS(caCert, cert, key string) AccessGrantWatcherOption {
	return func(w *AccessGrantWatcher) {
		w.tlsCACert = caCert
		w.tlsCert = cert
		w.tlsKey = key
	}
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
	if w.target == "" || w.nodeID == "" || w.nodeCredential == "" || w.cache == nil {
		cancel()
		return nil
	}
	if w.control == nil {
		control, err := newNodeControlClientProvider(w.target, w.tlsCACert, w.tlsCert, w.tlsKey)
		if err != nil {
			cancel()
			logrus.WithError(err).Warn("control-plane allocation access grant watcher disabled")
			return nil
		}
		w.control = control
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
				} else if next > revision {
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
		NodeID:         w.nodeID,
		AfterRevision:  afterRevision,
		NodeCredential: w.nodeCredential,
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
