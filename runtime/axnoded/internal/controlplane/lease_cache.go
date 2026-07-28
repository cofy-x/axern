package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"sync"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

const leaseWatchReconnectDelay = time.Second

type LeaseCache struct {
	mu      sync.RWMutex
	byToken map[string]*commonv1.ExecutionLease
	byLease map[string]string
	changed chan struct{}
}

func NewLeaseCache() *LeaseCache {
	return &LeaseCache{
		byToken: make(map[string]*commonv1.ExecutionLease),
		byLease: make(map[string]string),
		changed: make(chan struct{}),
	}
}

func (c *LeaseCache) Apply(leases []*commonv1.ExecutionLease) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	for tokenHash, lease := range c.byToken {
		if lease == nil || lease.GetExpiresAt() == nil || !lease.GetExpiresAt().AsTime().After(now) {
			delete(c.byToken, tokenHash)
			if leaseID := strings.TrimSpace(lease.GetLeaseID()); leaseID != "" && c.byLease[leaseID] == tokenHash {
				delete(c.byLease, leaseID)
			}
		}
	}
	applied := false
	for _, lease := range leases {
		if lease == nil || strings.TrimSpace(lease.GetAllocationID()) == "" {
			continue
		}
		tokenHash := strings.ToLower(strings.TrimSpace(lease.GetValidationTokenHash()))
		if tokenHash == "" {
			continue
		}
		leaseID := strings.TrimSpace(lease.GetLeaseID())
		if previous := c.byLease[leaseID]; leaseID != "" && previous != "" && previous != tokenHash {
			delete(c.byToken, previous)
		}
		c.byToken[tokenHash] = cloneLease(lease)
		if leaseID != "" {
			c.byLease[leaseID] = tokenHash
		}
		applied = true
	}
	if applied {
		close(c.changed)
		c.changed = make(chan struct{})
	}
}

func (c *LeaseCache) Validate(allocationID string, attempt int64, token string, now time.Time) bool {
	valid, _ := c.validationState(allocationID, attempt, token, now)
	return valid
}

func (c *LeaseCache) validationState(allocationID string, attempt int64, token string, now time.Time) (valid, known bool) {
	if c == nil || strings.TrimSpace(allocationID) == "" || attempt <= 0 || strings.TrimSpace(token) == "" {
		return false, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	hash := leaseTokenHash(token)
	lease, known := c.byToken[hash]
	if !known || lease == nil || lease.GetAttempt() != attempt || lease.GetAllocationID() != strings.TrimSpace(allocationID) {
		return false, false
	}
	return !lease.GetRevoked() && lease.GetExpiresAt() != nil && lease.GetExpiresAt().AsTime().After(now), true
}

func (c *LeaseCache) WaitValidate(ctx context.Context, allocationID string, attempt int64, token string, now func() time.Time) (bool, bool) {
	if c == nil || now == nil {
		return false, false
	}
	waited := false
	for {
		valid, known := c.validationState(allocationID, attempt, token, now())
		if valid || known {
			return valid, waited
		}
		c.mu.RLock()
		changed := c.changed
		c.mu.RUnlock()
		valid, known = c.validationState(allocationID, attempt, token, now())
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

type LeaseWatcher struct {
	target        string
	nodeID        string
	nodeAuthToken string
	cache         *LeaseCache
	control       NodeControlClientProvider
	tlsCACert     string
	tlsCert       string
	tlsKey        string
	ctx           context.Context
	cancel        context.CancelFunc

	stopCh    chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
	wg        sync.WaitGroup
}

type LeaseWatcherOption func(*LeaseWatcher)

func WithLeaseWatcherTarget(target string) LeaseWatcherOption {
	return func(w *LeaseWatcher) {
		w.target = strings.TrimSpace(target)
	}
}

func WithLeaseWatcherNode(nodeID, nodeAuthToken string) LeaseWatcherOption {
	return func(w *LeaseWatcher) {
		w.nodeID = strings.TrimSpace(nodeID)
		w.nodeAuthToken = strings.TrimSpace(nodeAuthToken)
	}
}

func WithLeaseWatcherTLS(caCert, cert, key string) LeaseWatcherOption {
	return func(w *LeaseWatcher) {
		w.tlsCACert = caCert
		w.tlsCert = cert
		w.tlsKey = key
	}
}

func WithLeaseWatcherCache(cache *LeaseCache) LeaseWatcherOption {
	return func(w *LeaseWatcher) {
		w.cache = cache
	}
}

func NewLeaseWatcher(options ...LeaseWatcherOption) *LeaseWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	w := &LeaseWatcher{
		stopCh: make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
	for _, option := range options {
		if option != nil {
			option(w)
		}
	}
	if w.target == "" || w.nodeID == "" || w.nodeAuthToken == "" || w.cache == nil {
		cancel()
		return nil
	}
	if w.control == nil {
		control, err := newNodeControlClientProvider(w.target, w.tlsCACert, w.tlsCert, w.tlsKey)
		if err != nil {
			cancel()
			logrus.WithError(err).Warn("control-plane lease watcher disabled")
			return nil
		}
		w.control = control
	}
	return w
}

func (w *LeaseWatcher) Start() {
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
					logrus.WithError(err).Warn("control-plane lease watch failed")
				} else if next > revision {
					revision = next
				}
				select {
				case <-time.After(leaseWatchReconnectDelay):
				case <-w.stopCh:
					return
				}
			}
		}()
	})
}

func (w *LeaseWatcher) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		w.cancel()
		close(w.stopCh)
		w.wg.Wait()
		if w.control != nil {
			if err := w.control.Close(); err != nil {
				logrus.WithError(err).Warn("close control-plane lease watcher client")
			}
		}
	})
}

func (w *LeaseWatcher) watchOnce(afterRevision int64) (int64, error) {
	client, err := w.control.Client(w.ctx)
	if err != nil {
		return afterRevision, err
	}
	stream, err := client.WatchExecutionLeases(w.ctx, &nodev1.WatchExecutionLeasesRequest{
		NodeID:        w.nodeID,
		AfterRevision: afterRevision,
		NodeAuthToken: w.nodeAuthToken,
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
		w.cache.Apply(resp.GetLeases())
		if resp.GetCurrentRevision() > afterRevision {
			afterRevision = resp.GetCurrentRevision()
		}
	}
}

func leaseTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func cloneLease(in *commonv1.ExecutionLease) *commonv1.ExecutionLease {
	if in == nil {
		return nil
	}
	cloned, ok := proto.Clone(in).(*commonv1.ExecutionLease)
	if !ok {
		return nil
	}
	return cloned
}
