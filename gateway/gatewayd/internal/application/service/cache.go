package service

import (
	"context"
	"fmt"
	"hash/maphash"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type Resolver interface {
	ResolveServiceRoute(ctx context.Context, in *gatewayv1.ResolveServiceRouteRequest) (*gatewayv1.ResolveServiceRouteResponse, error)
}

type Observer interface {
	RouteResolve(result string)
	RouteCache(event string)
	RouteCacheEntries(state string, value int)
	ObserveServiceProxyStage(stage, result, errorClass, method string, duration time.Duration)
}

type RouteRef struct {
	Namespace string
	ServiceID string
	PortRef   string
}

type Cache struct {
	resolver              Resolver
	ttl                   time.Duration
	maxEntries            int64
	observer              Observer
	obs                   *sdkobs.Handle
	endpointQuarantineTTL time.Duration
	hashSeed              maphash.Seed
	shards                []cacheShard
	// capacityMu serializes cross-shard pruning and eviction. Cache hits never acquire it.
	capacityMu  sync.Mutex
	metricsMu   sync.Mutex
	routes      atomic.Int64
	endpoints   atomic.Int64
	quarantined atomic.Int64
	inflight    atomic.Int64
}

const (
	minimumRouteLeaseTTL         = 30 * time.Second
	defaultRouteCacheTTL         = 3 * time.Second
	defaultRouteCacheMaxEntries  = 8192
	defaultEndpointQuarantineTTL = 30 * time.Second
	maximumRouteCacheShards      = 32
)

type Options struct {
	TTL                   time.Duration
	MaxEntries            int
	EndpointQuarantineTTL time.Duration
}

func NewCache(resolver Resolver, options Options, observer Observer, obs *sdkobs.Handle) *Cache {
	if options.TTL <= 0 {
		options.TTL = defaultRouteCacheTTL
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = defaultRouteCacheMaxEntries
	}
	if options.EndpointQuarantineTTL <= 0 {
		options.EndpointQuarantineTTL = defaultEndpointQuarantineTTL
	}
	shardCount := min(options.MaxEntries, maximumRouteCacheShards)
	cache := &Cache{
		resolver:              resolver,
		ttl:                   options.TTL,
		maxEntries:            int64(options.MaxEntries),
		observer:              observer,
		obs:                   obs,
		endpointQuarantineTTL: options.EndpointQuarantineTTL,
		hashSeed:              maphash.MakeSeed(),
		shards:                make([]cacheShard, shardCount),
	}
	for i := range cache.shards {
		cache.shards[i].init()
	}
	cache.observeState()
	return cache
}

func (c *Cache) Resolve(ctx context.Context, ref RouteRef) (*gatewayv1.ServiceRouteEndpoint, *gatewayv1.ServiceRoutePort, error) {
	ctx, span := c.obs.Start(ctx, observability.SpanRouteResolve,
		attribute.String(sdkobs.AttrServiceID, ref.ServiceID),
		attribute.String(sdkobs.AttrNamespace, ref.Namespace),
		attribute.String(sdkobs.AttrPortRef, ref.PortRef),
	)
	defer span.End()

	key := cacheKey(ref)
	shared := false
	for {
		resolveStart := time.Now()
		ep, port, ok, expired, selectDuration := c.resolveCachedRoute(key, resolveStart)
		if ok {
			c.observeCache("hit")
			c.observeStage("endpoint_select", "ok", "", selectDuration)
			result := "cache_hit"
			if shared {
				result = "shared_cache_hit"
			}
			c.observeStage("route_resolve", result, "", time.Since(resolveStart))
			span.SetAttributes(attribute.String(sdkobs.AttrResult, result))
			return ep, port, nil
		}
		if expired {
			c.observeCache("expired")
		} else {
			c.observeCache("miss")
		}

		call, leader := c.beginResolve(key)
		if !leader {
			waitStart := time.Now()
			if err := call.wait(ctx); err != nil {
				c.observeStage("route_resolve", "error", "shared", time.Since(waitStart))
				if ctx.Err() != nil {
					return nil, nil, ctx.Err()
				}
				if ep, port, ok, _, selectDuration := c.resolveCachedRoute(key, time.Now()); ok {
					c.observeCache("hit")
					c.observeStage("endpoint_select", "ok", "", selectDuration)
					c.observeStage("route_resolve", "shared_cache_hit", "", time.Since(waitStart))
					span.SetAttributes(attribute.String(sdkobs.AttrResult, "shared_cache_hit"))
					return ep, port, nil
				}
				return nil, nil, err
			}
			c.observeStage("route_resolve", "shared", "", time.Since(waitStart))
			shared = true
			continue
		}

		controlStart := time.Now()
		route, err := c.resolveRoute(ctx, ref)
		if err != nil {
			c.finishResolve(key, call, err)
			c.observeResolve("error")
			c.observeStage("route_resolve", "error", "control", time.Since(controlStart))
			span.RecordError(err)
			span.SetStatus(codes.Error, "route resolve failed")
			span.SetAttributes(attribute.String(sdkobs.AttrResult, "error"))
			return nil, nil, err
		}
		if len(route.GetEndpoints()) == 0 {
			err = fmt.Errorf("service route resolved with no endpoints: namespace=%q service_id=%q port=%q", ref.Namespace, ref.ServiceID, ref.PortRef)
			c.finishResolve(key, call, err)
			c.observeResolve("empty")
			c.observeStage("route_resolve", "error", "no_endpoints", time.Since(controlStart))
			span.RecordError(err)
			span.SetStatus(codes.Error, "route resolve returned no endpoints")
			span.SetAttributes(attribute.String(sdkobs.AttrResult, "empty"))
			return nil, nil, err
		}

		c.observeResolve("ok")
		c.observeStage("route_resolve", "ok", "", time.Since(controlStart))
		span.SetAttributes(attribute.String(sdkobs.AttrResult, "ok"))
		now := time.Now()
		selectStart := time.Now()
		ep = c.insertAndSelect(key, route, now)
		c.finishResolve(key, call, nil)
		c.observeStage("endpoint_select", "ok", "", time.Since(selectStart))
		return ep, route.GetPort(), nil
	}
}

func (c *Cache) resolveRoute(ctx context.Context, ref RouteRef) (*gatewayv1.ResolveServiceRouteResponse, error) {
	return c.resolver.ResolveServiceRoute(ctx, &gatewayv1.ResolveServiceRouteRequest{
		Namespace:  ref.Namespace,
		ServiceID:  ref.ServiceID,
		PortRef:    ref.PortRef,
		TtlSeconds: int64(maxDuration(c.ttl, minimumRouteLeaseTTL).Seconds()),
	})
}

func (c *Cache) ReportEndpointResult(ref RouteRef, ep *gatewayv1.ServiceRouteEndpoint, latency time.Duration, ok bool) {
	key := cacheKey(ref)
	endpoint := endpointKey(ep)
	if endpoint == "" {
		return
	}
	shard := c.shard(key)
	shard.mu.Lock()
	item := shard.items[key]
	if item == nil {
		shard.mu.Unlock()
		return
	}
	stats := item.endpointStats[endpoint]
	if stats == nil {
		shard.mu.Unlock()
		return
	}
	if stats.outstanding > 0 {
		stats.outstanding--
	}
	if latency > 0 {
		if !ok {
			latency += defaultEndpointErrorPenalty
		}
		stats.observe(latency)
	}
	shard.mu.Unlock()
}

func (c *Cache) Invalidate(ref RouteRef) {
	key := cacheKey(ref)
	shard := c.shard(key)
	shard.mu.Lock()
	delta := shard.removeLocked(shard.items[key])
	c.applyDelta(delta)
	shard.mu.Unlock()
	if !delta.empty() {
		c.observeState()
	}
	c.observeCache("invalidate")
}

func (c *Cache) QuarantineEndpoint(ref RouteRef, ep *gatewayv1.ServiceRouteEndpoint, _ string) {
	key := cacheKey(ref)
	endpoint := endpointKey(ep)
	if endpoint == "" || c.endpointQuarantineTTL <= 0 {
		return
	}
	shard := c.shard(key)
	shard.mu.Lock()
	item := shard.items[key]
	if item == nil {
		shard.mu.Unlock()
		return
	}
	delta := cacheDelta{}
	if _, exists := item.quarantined[endpoint]; !exists {
		delta.quarantined++
	}
	item.quarantined[endpoint] = time.Now().Add(c.endpointQuarantineTTL)
	c.applyDelta(delta)
	shard.mu.Unlock()
	if !delta.empty() {
		c.observeState()
	}
	c.observeCache("quarantine")
}

func (c *Cache) observeResolve(result string) {
	if c.observer != nil {
		c.observer.RouteResolve(result)
	}
}

func (c *Cache) observeCache(event string) {
	if c.observer != nil {
		c.observer.RouteCache(event)
	}
}

func (c *Cache) observeCacheN(event string, count int) {
	for range count {
		c.observeCache(event)
	}
}

func (c *Cache) observeStage(stage, result, errorClass string, duration time.Duration) {
	if c.observer != nil {
		c.observer.ObserveServiceProxyStage(stage, result, errorClass, "unknown", duration)
	}
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
