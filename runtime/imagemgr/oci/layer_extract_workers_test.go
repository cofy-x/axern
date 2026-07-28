package oci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestExtractLayersWithWorkers_RollsBackReservedRefsOnError(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()
	mgr.layerWorkers = 2

	goodHash, err := v1.NewHash("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatalf("new good hash: %v", err)
	}
	badHash, err := v1.NewHash("sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	if err != nil {
		t.Fatalf("new bad hash: %v", err)
	}

	_, _, err = mgr.extractLayersWithWorkers(context.Background(), []v1.Layer{
		sleepLayer{digest: goodHash},
		errorLayer{digest: badHash, err: fmt.Errorf("boom")},
	})
	if err == nil {
		t.Fatalf("extractLayersWithWorkers() error = nil, want non-nil")
	}

	layer, err := mgr.store.getLayer(goodHash.String())
	if err != nil {
		t.Fatalf("get good layer: %v", err)
	}
	if layer == nil {
		t.Fatalf("expected extracted good layer metadata to remain for cache reuse")
	}
	if layer.RefCount != 0 {
		t.Fatalf("expected reserved ref to be rolled back, got %d", layer.RefCount)
	}
	if layer.RefZeroAtUnix == 0 {
		t.Fatalf("expected rolled back layer to have ref-zero timestamp set")
	}
}

func TestExtractLayersWithWorkers_ConcurrentAndOrdered(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()
	mgr.layerWorkers = 2

	const layerCount = 4
	const delay = 200 * time.Millisecond

	layers := make([]v1.Layer, 0, layerCount)
	wantDigests := make([]string, 0, layerCount)
	for i := 0; i < layerCount; i++ {
		hash, err := v1.NewHash(fmt.Sprintf("sha256:%064x", i+1))
		if err != nil {
			t.Fatalf("new hash %d: %v", i, err)
		}
		layers = append(layers, sleepLayer{
			digest: hash,
			delay:  delay,
		})
		wantDigests = append(wantDigests, hash.String())
	}

	start := time.Now()
	gotDigests, gotPaths, err := mgr.extractLayersWithWorkers(context.Background(), layers)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("extractLayersWithWorkers() error: %v", err)
	}

	for i := 0; i < layerCount; i++ {
		if gotDigests[i] != wantDigests[i] {
			t.Fatalf("digest order mismatch at %d: got %s want %s", i, gotDigests[i], wantDigests[i])
		}
		if gotPaths[i] == "" {
			t.Fatalf("expected non-empty layer path at %d", i)
		}
		if _, err := os.Stat(gotPaths[i]); err != nil {
			t.Fatalf("layer path should exist at %d: %v", i, err)
		}
		layerDir := filepath.Base(filepath.Dir(gotPaths[i]))
		if len(layerDir) > 8 || layerDir == "" || layerDir[0] != 'l' {
			t.Fatalf("expected compact mapped layer dir, got %s", layerDir)
		}
	}

	if elapsed >= 700*time.Millisecond {
		t.Fatalf("expected concurrent extraction, elapsed=%v", elapsed)
	}
}

func TestExtractLayersWithWorkers_UseGlobalWorkerLimit(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()
	mgr.layerWorkers = 2

	var inFlight int32
	var maxFlight int32

	buildLayers := func(offset int) []v1.Layer {
		layers := make([]v1.Layer, 0, 3)
		for i := 0; i < 3; i++ {
			hash, err := v1.NewHash(fmt.Sprintf("sha256:%064x", offset+i+1))
			if err != nil {
				t.Fatalf("new hash: %v", err)
			}
			layers = append(layers, observedSleepLayer{
				digest:    hash,
				delay:     180 * time.Millisecond,
				inFlight:  &inFlight,
				maxFlight: &maxFlight,
			})
		}
		return layers
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	run := func(offset int) {
		defer wg.Done()
		_, _, err := mgr.extractLayersWithWorkers(context.Background(), buildLayers(offset))
		errCh <- err
	}

	wg.Add(2)
	go run(0)
	go run(100)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("extractLayersWithWorkers() error: %v", err)
		}
	}

	if got := atomic.LoadInt32(&maxFlight); got > int32(mgr.layerWorkers) {
		t.Fatalf("expected global max concurrency <= %d, got %d", mgr.layerWorkers, got)
	}
	if got := atomic.LoadInt32(&maxFlight); got < 2 {
		t.Fatalf("expected observed concurrency >= 2, got %d", got)
	}
}

func TestClose_WaitsForInFlightLayerWorker(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	hash, err := v1.NewHash("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("new hash: %v", err)
	}

	started := make(chan struct{}, 1)
	unblock := make(chan struct{})
	layer := blockLayer{
		digest:  hash,
		started: started,
		unblock: unblock,
	}

	extractDone := make(chan error, 1)
	go func() {
		_, _, err := mgr.extractLayersWithWorkers(context.Background(), []v1.Layer{layer})
		extractDone <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("worker did not start layer extraction in time")
	}

	closeDone := make(chan struct{})
	go func() {
		_ = mgr.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		t.Fatalf("Close() returned before in-flight worker finished")
	case <-time.After(120 * time.Millisecond):
	}

	close(unblock)

	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("Close() did not finish after worker unblocked")
	}

	select {
	case <-extractDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("extract call did not finish")
	}
}
