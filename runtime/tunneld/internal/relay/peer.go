package relay

import (
	"sync"
	"sync/atomic"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

type peer struct {
	kind      tunnelcontrolv1.TunnelPeerKind
	sessionID string
	token     string
	send      chan *tunnelv1.TunnelFrame
	done      chan struct{}
	once      sync.Once
	closeErr  error
	pair      *pair
	lastSeen  atomicTime
	bytesIn   atomic.Int64
	bytesOut  atomic.Int64
}

type pair struct {
	client   atomic.Pointer[peer]
	node     atomic.Pointer[peer]
	signalMu sync.Mutex
	changed  chan struct{}
}

type atomicTime struct {
	nano atomic.Int64
}

func (p *peer) close() {
	p.closeWithError(nil)
}

func (p *peer) closeWithError(err error) {
	p.once.Do(func() {
		// Closing done publishes the first terminal cause to every waiter.
		p.closeErr = err
		close(p.done)
	})
}

func (p *peer) error() error {
	return p.closeErr
}

func (p *peer) run(loop func() error) {
	p.closeWithError(loop())
}

func (t *atomicTime) Store(v time.Time) {
	t.nano.Store(v.UnixNano())
}

func (t *atomicTime) Load() time.Time {
	nano := t.nano.Load()
	if nano == 0 {
		return time.Now()
	}
	return time.Unix(0, nano)
}
