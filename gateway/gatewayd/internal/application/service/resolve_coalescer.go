package service

import "context"

type resolveCall struct {
	done chan struct{}
	err  error
}

func (c *Cache) beginResolve(key string) (*resolveCall, bool) {
	shard := c.shard(key)
	shard.mu.Lock()
	if call := shard.inflight[key]; call != nil {
		shard.mu.Unlock()
		return call, false
	}
	call := &resolveCall{done: make(chan struct{})}
	shard.inflight[key] = call
	c.applyDelta(cacheDelta{inflight: 1})
	shard.mu.Unlock()
	c.observeState()
	return call, true
}

func (c *Cache) finishResolve(key string, call *resolveCall, err error) {
	shard := c.shard(key)
	shard.mu.Lock()
	if shard.inflight[key] != call {
		shard.mu.Unlock()
		return
	}
	call.err = err
	delete(shard.inflight, key)
	c.applyDelta(cacheDelta{inflight: -1})
	close(call.done)
	shard.mu.Unlock()
	c.observeState()
}

func (c *resolveCall) wait(ctx context.Context) error {
	select {
	case <-c.done:
		return c.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
