package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
)

type fakeResolver struct {
	mu        sync.Mutex
	enterOnce sync.Once
	entered   chan struct{}
	calls     int
	delay     time.Duration
	resp      *gatewayv1.ResolveServiceRouteResponse
	err       error
}

type observedStage struct {
	stage      string
	result     string
	errorClass string
}

type fakeObserver struct {
	mu     sync.Mutex
	stages []observedStage
}

func (*fakeObserver) RouteResolve(string) {}

func (*fakeObserver) RouteCache(string) {}

func (*fakeObserver) RouteCacheEntries(string, int) {}

func (f *fakeObserver) ObserveServiceProxyStage(stage, result, errorClass, _ string, _ time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stages = append(f.stages, observedStage{stage: stage, result: result, errorClass: errorClass})
}

func (f *fakeObserver) stagesNamed(name string) []observedStage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var stages []observedStage
	for _, stage := range f.stages {
		if stage.stage == name {
			stages = append(stages, stage)
		}
	}
	return stages
}

func (f *fakeResolver) ResolveServiceRoute(context.Context, *gatewayv1.ResolveServiceRouteRequest) (*gatewayv1.ResolveServiceRouteResponse, error) {
	if f.entered != nil {
		f.enterOnce.Do(func() { close(f.entered) })
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return testRouteResponse(), nil
}

func testRouteResponse() *gatewayv1.ResolveServiceRouteResponse {
	return &gatewayv1.ResolveServiceRouteResponse{
		Port: &gatewayv1.ServiceRoutePort{ContainerPort: 8080, Protocol: commonv1.PortProtocol_PORT_PROTOCOL_TCP},
		Endpoints: []*gatewayv1.ServiceRouteEndpoint{
			{AllocationID: "alloc-a", ContainerPort: 8080},
			{AllocationID: "alloc-b", ContainerPort: 8080},
		},
	}
}

func (f *fakeResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newTestCache(resolver Resolver, ttl, quarantineTTL time.Duration) *Cache {
	return NewCache(resolver, Options{
		TTL:                   ttl,
		MaxEntries:            128,
		EndpointQuarantineTTL: quarantineTTL,
	}, nil, nil)
}

func TestCacheRejectsEmptyResolveResponse(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{resp: &gatewayv1.ResolveServiceRouteResponse{}}
	cache := newTestCache(resolver, time.Minute, time.Minute)
	if ep, port, err := cache.Resolve(context.Background(), RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}); err == nil || ep != nil || port != nil {
		t.Fatalf("Resolve() = ep:%#v port:%#v err:%v, want empty route error", ep, port, err)
	}
}

func TestCacheUsesTTLAndRotatesEndpoints(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := newTestCache(resolver, time.Minute, time.Minute)
	path := RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}
	first, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.callCount() != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.callCount())
	}
	if first.GetAllocationID() == second.GetAllocationID() {
		t.Fatalf("expected endpoint rotation, got %q twice", first.GetAllocationID())
	}
}

func TestCacheSelectsLeastOutstandingEndpoint(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := newTestCache(resolver, time.Minute, time.Minute)
	path := RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}

	first, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetAllocationID() == second.GetAllocationID() {
		t.Fatalf("Resolve() selected %q twice while first request was outstanding", first.GetAllocationID())
	}
}

func TestCacheSelectsLowerEWMAEndpoint(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := newTestCache(resolver, time.Minute, time.Minute)
	path := RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}

	first, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	cache.ReportEndpointResult(path, first, 10*time.Millisecond, true)
	cache.ReportEndpointResult(path, second, 200*time.Millisecond, true)

	next, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if next.GetAllocationID() != first.GetAllocationID() {
		t.Fatalf("Resolve() = %q, want lower EWMA endpoint %q", next.GetAllocationID(), first.GetAllocationID())
	}
}

func TestCacheBoundsEndpointStarvationAfterLatencyObservations(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := newTestCache(resolver, time.Minute, time.Minute)
	path := RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}

	seen := make(map[string]int)
	for range 12 {
		endpoint, _, err := cache.Resolve(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		seen[endpoint.GetAllocationID()]++
		cache.ReportEndpointResult(path, endpoint, time.Duration(len(seen))*time.Millisecond, true)
	}

	if len(seen) != 2 {
		t.Fatalf("selected endpoints = %d, want 2", len(seen))
	}
	for endpoint, selections := range seen {
		if selections != 6 {
			t.Fatalf("endpoint %s selected %d times, want 6", endpoint, selections)
		}
	}
}

func TestCacheInvalidateForcesResolve(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := newTestCache(resolver, time.Minute, time.Minute)
	path := RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}
	if _, _, err := cache.Resolve(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	cache.Invalidate(path)
	if _, _, err := cache.Resolve(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if resolver.callCount() != 2 {
		t.Fatalf("resolver calls = %d, want 2", resolver.callCount())
	}
}

func TestCacheCoalescesConcurrentResolve(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{delay: 25 * time.Millisecond}
	cache := newTestCache(resolver, time.Minute, time.Minute)
	path := RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}
	const workers = 20

	var wg sync.WaitGroup
	endpoints := make(chan string, workers)
	errors := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ep, _, err := cache.Resolve(context.Background(), path)
			if err != nil {
				errors <- err
				return
			}
			endpoints <- ep.GetAllocationID()
		}()
	}
	wg.Wait()
	close(endpoints)
	close(errors)

	for err := range errors {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolver.callCount() != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.callCount())
	}
	seen := map[string]bool{}
	for endpoint := range endpoints {
		seen[endpoint] = true
	}
	if !seen["alloc-a"] || !seen["alloc-b"] {
		t.Fatalf("endpoint rotation after shared resolve saw %v, want alloc-a and alloc-b", seen)
	}
}

func TestCacheSkipsQuarantinedEndpoint(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := newTestCache(resolver, time.Minute, time.Minute)
	path := RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}
	first, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	cache.QuarantineEndpoint(path, first, "timeout")

	second, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if second.GetAllocationID() == first.GetAllocationID() {
		t.Fatalf("Resolve() returned quarantined endpoint %q", first.GetAllocationID())
	}
}

func TestCacheUsesEndpointAfterQuarantineExpires(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := newTestCache(resolver, time.Minute, time.Nanosecond)
	path := RouteRef{Namespace: "default", ServiceID: "svc-123", PortRef: "8080"}
	first, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	cache.QuarantineEndpoint(path, first, "timeout")
	time.Sleep(time.Millisecond)

	_, _, err = cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := cache.Resolve(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if next.GetAllocationID() != first.GetAllocationID() {
		t.Fatalf("Resolve() = %q, want expired quarantine endpoint %q", next.GetAllocationID(), first.GetAllocationID())
	}
}

func TestCacheBoundsRouteAndEndpointState(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := NewCache(resolver, Options{
		TTL:                   time.Minute,
		MaxEntries:            3,
		EndpointQuarantineTTL: time.Minute,
	}, nil, nil)

	for i := 0; i < 10; i++ {
		ref := RouteRef{Namespace: "default", ServiceID: fmt.Sprintf("svc-%d", i), PortRef: "8080"}
		endpoint, _, err := cache.Resolve(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		cache.ReportEndpointResult(ref, endpoint, time.Millisecond, true)
		cache.QuarantineEndpoint(ref, endpoint, "test")
	}

	internal := inspectCache(cache)
	if got := internal.items; got != 3 {
		t.Fatalf("route entries = %d, want 3", got)
	}
	if got := internal.endpointStatRoutes; got != 3 {
		t.Fatalf("endpoint stat routes = %d, want 3", got)
	}
	if got := internal.quarantinedRoutes; got != 3 {
		t.Fatalf("quarantined routes = %d, want 3", got)
	}
	if got := internal.lru; got != 3 {
		t.Fatalf("LRU entries = %d, want 3", got)
	}
	if got := internal.expirations; got != 3 {
		t.Fatalf("expiration entries = %d, want 3", got)
	}
	if got := cache.state().endpoints; got != 6 {
		t.Fatalf("endpoint entries = %d, want 6", got)
	}
	if got := cache.state().quarantined; got != 3 {
		t.Fatalf("quarantine entries = %d, want 3", got)
	}
}

func TestCacheExpirationRemovesAssociatedState(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{}
	cache := NewCache(resolver, Options{
		TTL:                   time.Millisecond,
		MaxEntries:            8,
		EndpointQuarantineTTL: time.Minute,
	}, nil, nil)
	first := RouteRef{Namespace: "default", ServiceID: "svc-expired", PortRef: "8080"}
	endpoint, _, err := cache.Resolve(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	cache.QuarantineEndpoint(first, endpoint, "test")
	time.Sleep(5 * time.Millisecond)

	current := refOnDifferentShard(cache, first, "svc-current")
	if _, _, err := cache.Resolve(context.Background(), current); err != nil {
		t.Fatal(err)
	}

	key := cacheKey(first)
	shard := cache.shard(key)
	shard.mu.Lock()
	item := shard.items[key]
	shard.mu.Unlock()
	if item != nil {
		t.Fatal("expired route remains cached")
	}
	state := cache.state()
	if state.endpoints != 2 || state.quarantined != 0 {
		t.Fatalf("remaining state counters = endpoints:%d quarantined:%d, want 2/0", state.endpoints, state.quarantined)
	}
	if got := inspectCache(cache).expirations; got != 1 {
		t.Fatalf("expiration entries = %d, want 1", got)
	}
}

func TestCacheDifferentShardsDoNotShareHitLock(t *testing.T) {
	t.Parallel()
	cache := newTestCache(&fakeResolver{}, time.Minute, time.Minute)
	first := RouteRef{Namespace: "default", ServiceID: "svc-locked", PortRef: "8080"}
	second := refOnDifferentShard(cache, first, "svc-independent")
	for _, ref := range []RouteRef{first, second} {
		if _, _, err := cache.Resolve(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
	}

	locked := cache.shard(cacheKey(first))
	locked.mu.Lock()
	done := make(chan error, 1)
	go func() {
		_, _, err := cache.Resolve(context.Background(), second)
		done <- err
	}()
	select {
	case err := <-done:
		locked.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		locked.mu.Unlock()
		t.Fatal("cache hit on a different shard blocked behind unrelated route lock")
	}
}

func TestCacheConcurrentInsertionsRespectGlobalCapacity(t *testing.T) {
	t.Parallel()
	cache := NewCache(&fakeResolver{}, Options{
		TTL:                   time.Minute,
		MaxEntries:            7,
		EndpointQuarantineTTL: time.Minute,
	}, nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ref := RouteRef{Namespace: "default", ServiceID: fmt.Sprintf("svc-concurrent-%d", i), PortRef: "8080"}
			if _, _, err := cache.Resolve(context.Background(), ref); err != nil {
				t.Errorf("Resolve(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if got := cache.state().routes; got != 7 {
		t.Fatalf("route entries = %d, want 7", got)
	}
	internal := inspectCache(cache)
	if internal.items != 7 || internal.lru != 7 || internal.expirations != 7 {
		t.Fatalf("bounded internals = items:%d lru:%d expirations:%d, want 7/7/7", internal.items, internal.lru, internal.expirations)
	}
}

func TestCacheWaiterCancellationDoesNotCancelLeader(t *testing.T) {
	t.Parallel()
	resolver := newControlledResolver(nil)
	cache := newTestCache(resolver, time.Minute, time.Minute)
	ref := RouteRef{Namespace: "default", ServiceID: "svc-cancel", PortRef: "8080"}
	leaderDone := make(chan error, 1)
	go func() {
		_, _, err := cache.Resolve(context.Background(), ref)
		leaderDone <- err
	}()
	<-resolver.entered

	waiterCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, _, err := cache.Resolve(waiterCtx, ref); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error = %v, want deadline exceeded", err)
	}
	close(resolver.release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader error = %v", err)
	}
	if resolver.callCount() != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.callCount())
	}
	if state := cache.state(); state.inflight != 0 || state.routes != 1 {
		t.Fatalf("cache state after waiter cancellation = %+v, want inflight 0 and routes 1", state)
	}
}

func TestCacheBroadcastsResolverErrorToWaiters(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("resolver unavailable")
	resolver := newControlledResolver(wantErr)
	cache := newTestCache(resolver, time.Minute, time.Minute)
	ref := RouteRef{Namespace: "default", ServiceID: "svc-error", PortRef: "8080"}

	const workers = 20
	start := make(chan struct{})
	errorsSeen := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := cache.Resolve(context.Background(), ref)
			errorsSeen <- err
		}()
	}
	close(start)
	<-resolver.entered
	time.Sleep(20 * time.Millisecond)
	close(resolver.release)
	wg.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if !errors.Is(err, wantErr) {
			t.Fatalf("Resolve() error = %v, want %v", err, wantErr)
		}
	}
	if resolver.callCount() != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.callCount())
	}
	if state := cache.state(); state.inflight != 0 || state.routes != 0 {
		t.Fatalf("cache state after resolver error = %+v, want empty", state)
	}
}

func TestCacheInvalidateDuringResolveLeavesConsistentState(t *testing.T) {
	t.Parallel()
	resolver := newControlledResolver(nil)
	cache := newTestCache(resolver, time.Minute, time.Minute)
	ref := RouteRef{Namespace: "default", ServiceID: "svc-inflight", PortRef: "8080"}
	done := make(chan error, 1)
	go func() {
		_, _, err := cache.Resolve(context.Background(), ref)
		done <- err
	}()
	<-resolver.entered
	cache.Invalidate(ref)
	close(resolver.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if state := cache.state(); state.inflight != 0 || state.routes != 1 || state.endpoints != 2 {
		t.Fatalf("cache state after concurrent invalidate = %+v, want one complete route", state)
	}
}

func TestCacheRouteUpdatePrunesRemovedEndpointState(t *testing.T) {
	t.Parallel()
	cache := newTestCache(&fakeResolver{}, time.Minute, time.Minute)
	ref := RouteRef{Namespace: "default", ServiceID: "svc-update", PortRef: "8080"}
	first, _, err := cache.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	cache.QuarantineEndpoint(ref, first, "test")

	updated := &gatewayv1.ResolveServiceRouteResponse{
		Port: &gatewayv1.ServiceRoutePort{ContainerPort: 8080, Protocol: commonv1.PortProtocol_PORT_PROTOCOL_TCP},
		Endpoints: []*gatewayv1.ServiceRouteEndpoint{
			{AllocationID: "alloc-new", ContainerPort: 8080},
		},
	}
	cache.insertAndSelect(cacheKey(ref), updated, time.Now())
	state := cache.state()
	if state.routes != 1 || state.endpoints != 1 || state.quarantined != 0 {
		t.Fatalf("cache state after route update = %+v, want routes/endpoints/quarantined 1/1/0", state)
	}
	shard := cache.shard(cacheKey(ref))
	shard.mu.Lock()
	_, oldStats := shard.items[cacheKey(ref)].endpointStats[endpointKey(first)]
	shard.mu.Unlock()
	if oldStats {
		t.Fatal("removed endpoint stats remain after route update")
	}
}

type cacheInternals struct {
	items              int
	endpointStatRoutes int
	quarantinedRoutes  int
	lru                int
	expirations        int
}

func inspectCache(cache *Cache) cacheInternals {
	result := cacheInternals{}
	for i := range cache.shards {
		shard := &cache.shards[i]
		shard.mu.Lock()
		result.items += len(shard.items)
		result.lru += shard.lru.Len()
		result.expirations += shard.expirations.Len()
		for _, item := range shard.items {
			if len(item.endpointStats) > 0 {
				result.endpointStatRoutes++
			}
			if len(item.quarantined) > 0 {
				result.quarantinedRoutes++
			}
		}
		shard.mu.Unlock()
	}
	return result
}

func refOnDifferentShard(cache *Cache, ref RouteRef, prefix string) RouteRef {
	wantDifferentFrom := cache.shard(cacheKey(ref))
	for i := 0; ; i++ {
		candidate := RouteRef{Namespace: ref.Namespace, ServiceID: fmt.Sprintf("%s-%d", prefix, i), PortRef: ref.PortRef}
		if cache.shard(cacheKey(candidate)) != wantDifferentFrom {
			return candidate
		}
	}
}

type controlledResolver struct {
	mu      sync.Mutex
	calls   int
	err     error
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newControlledResolver(err error) *controlledResolver {
	return &controlledResolver{err: err, entered: make(chan struct{}), release: make(chan struct{})}
}

func (r *controlledResolver) ResolveServiceRoute(context.Context, *gatewayv1.ResolveServiceRouteRequest) (*gatewayv1.ResolveServiceRouteResponse, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	r.once.Do(func() { close(r.entered) })
	<-r.release
	if r.err != nil {
		return nil, r.err
	}
	return testRouteResponse(), nil
}

func (r *controlledResolver) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}
