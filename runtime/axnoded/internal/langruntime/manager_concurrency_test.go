package langruntime

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Scenario 1: N goroutines doing slow Add should NOT block concurrent Get for
// already-registered runtimes.
func TestScenario1_GetNotBlockedBySlowAdd(t *testing.T) {
	const (
		numAdders  = 256
		numGetters = 256
		mountDelay = 500 * time.Millisecond
	)

	mock := &mockMounter{mountDelay: mountDelay}
	lm := NewLanguageRuntimeManager(mock)

	const preRegistered = 32
	var setupWg sync.WaitGroup
	setupWg.Add(preRegistered)
	for i := 0; i < preRegistered; i++ {
		go func(idx int) {
			defer setupWg.Done()
			addTestLangRuntime(lm, newTestFR(fmt.Sprintf("existing-%d", idx), fmt.Sprintf("/existing/%d", idx)), false)
		}(i)
	}
	setupWg.Wait()

	var adderWg sync.WaitGroup
	adderWg.Add(numAdders)
	for i := 0; i < numAdders; i++ {
		go func(idx int) {
			defer adderWg.Done()
			addTestLangRuntime(lm, newTestFR(
				fmt.Sprintf("slow-%d", idx),
				fmt.Sprintf("/slow/%d", idx),
			), false)
		}(i)
	}

	time.Sleep(50 * time.Millisecond)

	var getterWg sync.WaitGroup
	getterWg.Add(numGetters)
	var maxGetLatency atomic.Int64

	for i := 0; i < numGetters; i++ {
		go func(idx int) {
			defer getterWg.Done()
			id := fmt.Sprintf("existing-%d", idx%preRegistered)
			start := time.Now()
			lr := lm.GetLangRuntime(id)
			latency := time.Since(start)
			if lr == nil {
				t.Errorf("GetLangRuntime(%s) returned nil", id)
				return
			}
			for {
				cur := maxGetLatency.Load()
				ns := int64(latency)
				if ns <= cur {
					break
				}
				if maxGetLatency.CompareAndSwap(cur, ns) {
					break
				}
			}
		}(i)
	}

	getterWg.Wait()

	maxLatency := time.Duration(maxGetLatency.Load())
	t.Logf("max Get latency: %v (mount delay: %v, adders: %d, getters: %d)", maxLatency, mountDelay, numAdders, numGetters)
	if maxLatency > 50*time.Millisecond {
		t.Fatalf("Get latency %v too high, Get is being blocked by slow Add", maxLatency)
	}

	adderWg.Wait()

	total := len(lm.List())
	expected := preRegistered + numAdders
	if total != expected {
		t.Fatalf("expected %d runtimes, got %d", expected, total)
	}
}

// Scenario 2: N goroutines adding runtimes with different rootfs configs should
// run in parallel, not be serialized by rfMu.
func TestScenario2_DifferentRootfsParallel(t *testing.T) {
	const (
		numAdders  = 256
		mountDelay = 200 * time.Millisecond
	)

	mock := &mockMounter{mountDelay: mountDelay}
	lm := NewLanguageRuntimeManager(mock)

	var wg sync.WaitGroup
	wg.Add(numAdders)

	start := time.Now()
	for i := 0; i < numAdders; i++ {
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("rt-%d", idx)
			path := fmt.Sprintf("/path/%d", idx)
			if _, err := addTestLangRuntime(lm, newTestFR(id, path), false); err != nil {
				t.Errorf("AddLangRuntime %s failed: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	maxExpected := 3 * time.Second
	t.Logf("elapsed: %v (mount delay: %v, adders: %d, serial would be: %v)", elapsed, mountDelay, numAdders, time.Duration(numAdders)*mountDelay)
	if elapsed > maxExpected {
		t.Fatalf("concurrent adds took %v (> %v), not running in parallel", elapsed, maxExpected)
	}
	if mock.MountCount() != numAdders {
		t.Fatalf("expected %d mount calls, got %d", numAdders, mock.MountCount())
	}
	if len(lm.List()) != numAdders {
		t.Fatalf("expected %d runtimes, got %d", numAdders, len(lm.List()))
	}
}

// Scenario 3: N goroutines adding runtimes with the SAME rootfs config should
// share one rootfs and only trigger one mount (future/singleflight pattern).
func TestScenario3_SameRootfsSingleMount(t *testing.T) {
	const (
		numAdders  = 512
		mountDelay = 200 * time.Millisecond
	)

	mock := &mockMounter{mountDelay: mountDelay}
	lm := NewLanguageRuntimeManager(mock)

	var wg sync.WaitGroup
	wg.Add(numAdders)
	results := make([]*LanguageRuntime, numAdders)
	errs := make([]error, numAdders)

	start := time.Now()
	for i := 0; i < numAdders; i++ {
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("rt-%d", idx)
			results[idx], errs[idx] = addTestLangRuntime(lm, newTestFR(id, "/shared/rootfs"), false)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d failed: %v", i, err)
		}
	}

	sharedRootFS := results[0].RootFS
	for i := 1; i < numAdders; i++ {
		if results[i].RootFS != sharedRootFS {
			t.Fatalf("goroutine %d got different rootfs pointer", i)
		}
	}

	if mock.MountCount() != 1 {
		t.Fatalf("expected 1 mount call, got %d", mock.MountCount())
	}

	maxExpected := 2 * time.Second
	t.Logf("elapsed: %v (mount delay: %v, adders: %d)", elapsed, mountDelay, numAdders)
	if elapsed > maxExpected {
		t.Fatalf("took %v (> %v), singleflight not working", elapsed, maxExpected)
	}

	if len(lm.List()) != numAdders {
		t.Fatalf("expected %d runtimes, got %d", numAdders, len(lm.List()))
	}
}

// TestHighConcurrencyMixed runs a mixed workload of adds (with varied rootfs),
// gets, and lists concurrently at high parallelism to stress-test for races.
func TestHighConcurrencyMixed(t *testing.T) {
	const (
		preRegistered = 32
		numAdders     = 256
		numGetters    = 256
		numListers    = 64
	)

	mock := &mockMounter{mountDelay: 5 * time.Millisecond}
	lm := NewLanguageRuntimeManager(mock)

	for i := 0; i < preRegistered; i++ {
		addTestLangRuntime(lm, newTestFR(fmt.Sprintf("pre-%d", i), fmt.Sprintf("/pre/%d", i)), false)
	}

	var wg sync.WaitGroup
	var totalOps atomic.Int64

	wg.Add(numAdders)
	for i := 0; i < numAdders; i++ {
		go func(idx int) {
			defer wg.Done()
			var id, path string
			switch {
			case idx%3 == 0:
				id = fmt.Sprintf("unique-%d", idx)
				path = fmt.Sprintf("/unique/%d", idx)
			case idx%3 == 1:
				bucket := idx % 8
				id = fmt.Sprintf("shared-%d", idx)
				path = fmt.Sprintf("/shared/%d", bucket)
			default:
				slot := idx % 16
				id = fmt.Sprintf("dup-%d", slot)
				path = fmt.Sprintf("/dup/%d", slot)
			}
			addTestLangRuntime(lm, newTestFR(id, path), false)
			totalOps.Add(1)
		}(i)
	}

	wg.Add(numGetters)
	for i := 0; i < numGetters; i++ {
		go func(idx int) {
			defer wg.Done()
			lm.GetLangRuntime(fmt.Sprintf("pre-%d", idx%preRegistered))
			totalOps.Add(1)
		}(i)
	}

	wg.Add(numListers)
	for i := 0; i < numListers; i++ {
		go func() {
			defer wg.Done()
			lm.List()
			totalOps.Add(1)
		}()
	}

	wg.Wait()

	expected := int64(numAdders + numGetters + numListers)
	if totalOps.Load() != expected {
		t.Fatalf("expected %d ops, got %d", expected, totalOps.Load())
	}
	t.Logf("completed %d ops (adders=%d, getters=%d, listers=%d), final runtimes=%d",
		totalOps.Load(), numAdders, numGetters, numListers, len(lm.List()))
}
