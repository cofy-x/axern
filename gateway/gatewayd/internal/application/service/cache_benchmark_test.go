package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkCacheHitSingleRouteParallel(b *testing.B) {
	cache := newTestCache(&fakeResolver{}, time.Minute, time.Minute)
	ref := RouteRef{Namespace: "default", ServiceID: "svc-single", PortRef: "8080"}
	if _, _, err := cache.Resolve(context.Background(), ref); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ep, _, err := cache.Resolve(context.Background(), ref)
			if err != nil {
				b.Error(err)
				continue
			}
			cache.ReportEndpointResult(ref, ep, time.Millisecond, true)
		}
	})
}

func BenchmarkCacheHitManyRoutesParallel(b *testing.B) {
	cache := NewCache(&fakeResolver{}, Options{
		TTL:                   time.Minute,
		MaxEntries:            2048,
		EndpointQuarantineTTL: time.Minute,
	}, nil, nil)
	refs := make([]RouteRef, 1024)
	for i := range refs {
		refs[i] = RouteRef{Namespace: "default", ServiceID: fmt.Sprintf("svc-%d", i), PortRef: "8080"}
		if _, _, err := cache.Resolve(context.Background(), refs[i]); err != nil {
			b.Fatal(err)
		}
	}
	var sequence atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ref := refs[sequence.Add(1)%uint64(len(refs))]
			ep, _, err := cache.Resolve(context.Background(), ref)
			if err != nil {
				b.Error(err)
				continue
			}
			cache.ReportEndpointResult(ref, ep, time.Millisecond, true)
		}
	})
}

func BenchmarkCacheResolveReportMixedParallel(b *testing.B) {
	cache := newTestCache(&fakeResolver{}, time.Minute, time.Minute)
	refs := []RouteRef{
		{Namespace: "default", ServiceID: "svc-a", PortRef: "8080"},
		{Namespace: "default", ServiceID: "svc-b", PortRef: "8080"},
		{Namespace: "default", ServiceID: "svc-c", PortRef: "8080"},
		{Namespace: "default", ServiceID: "svc-d", PortRef: "8080"},
	}
	for _, ref := range refs {
		if _, _, err := cache.Resolve(context.Background(), ref); err != nil {
			b.Fatal(err)
		}
	}
	var sequence atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ref := refs[sequence.Add(1)%uint64(len(refs))]
			ep, _, err := cache.Resolve(context.Background(), ref)
			if err != nil {
				b.Error(err)
				continue
			}
			cache.ReportEndpointResult(ref, ep, time.Millisecond, true)
		}
	})
}

func BenchmarkCacheColdResolveCoalesced(b *testing.B) {
	for range b.N {
		resolver := &fakeResolver{delay: 5 * time.Millisecond}
		cache := newTestCache(resolver, time.Minute, time.Minute)
		ref := RouteRef{Namespace: "default", ServiceID: "svc-cold", PortRef: "8080"}
		var wg sync.WaitGroup
		wg.Add(32)
		for range 32 {
			go func() {
				defer wg.Done()
				if _, _, err := cache.Resolve(context.Background(), ref); err != nil {
					b.Error(err)
				}
			}()
		}
		wg.Wait()
	}
}
