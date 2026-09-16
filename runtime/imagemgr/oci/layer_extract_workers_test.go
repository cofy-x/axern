package oci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	mgr.layerWorkers = 2

	const layerCount = 4
	started := make([]chan struct{}, layerCount)
	release := make([]chan struct{}, layerCount)
	unblock := func(i int) {
		if release[i] != nil {
			close(release[i])
			release[i] = nil
		}
	}
	done := make(chan struct{})

	layers := make([]v1.Layer, 0, layerCount)
	wantDigests := make([]string, 0, layerCount)
	for i := 0; i < layerCount; i++ {
		hash, err := v1.NewHash(fmt.Sprintf("sha256:%064x", i+1))
		if err != nil {
			t.Fatalf("new hash %d: %v", i, err)
		}
		started[i] = make(chan struct{}, 1)
		release[i] = make(chan struct{})
		layers = append(layers, blockLayer{
			digest:  hash,
			started: started[i],
			unblock: release[i],
		})
		wantDigests = append(wantDigests, hash.String())
	}

	var gotDigests, gotPaths []string
	var err error
	// Release every barrier even after a failed assertion, then join the
	// extraction and workers before closing their store and temporary files.
	t.Cleanup(func() {
		for i := range release {
			unblock(i)
		}
		<-done
		if err := mgr.Close(); err != nil {
			t.Errorf("close manager: %v", err)
		}
	})
	go func() {
		defer close(done)
		gotDigests, gotPaths, err = mgr.extractLayersWithWorkers(context.Background(), layers)
	}()
	wait := func(ch <-chan struct{}, label string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for %s", label)
		}
	}
	// Both workers must enter extraction before either is released. A serial
	// implementation cannot satisfy this barrier, regardless of machine speed.
	wait(started[0], "first layer")
	wait(started[1], "second layer")
	// Keep the first layer blocked while the other worker completes layers
	// 1 and 2 and starts layer 3. Completion order must not change result order.
	unblock(1)
	wait(started[2], "third layer")
	unblock(2)
	wait(started[3], "fourth layer")
	unblock(3)
	unblock(0)
	wait(done, "extraction completion")
	if err != nil {
		t.Fatalf("extractLayersWithWorkers() error: %v", err)
	}
	if len(gotDigests) != layerCount || len(gotPaths) != layerCount {
		t.Fatalf("result lengths = (%d, %d), want (%d, %d)", len(gotDigests), len(gotPaths), layerCount, layerCount)
	}

	for i := 0; i < layerCount; i++ {
		if gotDigests[i] != wantDigests[i] {
			t.Fatalf("digest order mismatch at %d: got %s want %s", i, gotDigests[i], wantDigests[i])
		}
		if gotPaths[i] == "" {
			t.Fatalf("expected non-empty layer path at %d", i)
		}
		record, err := mgr.store.getLayer(wantDigests[i])
		if err != nil {
			t.Fatalf("read layer metadata at %d: %v", i, err)
		}
		if record == nil || gotPaths[i] != record.Path {
			t.Fatalf("path order mismatch at %d: path %q does not match layer %s", i, gotPaths[i], wantDigests[i])
		}
		if _, err := os.Stat(gotPaths[i]); err != nil {
			t.Fatalf("layer path should exist at %d: %v", i, err)
		}
		layerDir := filepath.Base(filepath.Dir(gotPaths[i]))
		if len(layerDir) != 66 || !strings.HasPrefix(layerDir, "l-") {
			t.Fatalf("expected content-addressed layer dir, got %s", layerDir)
		}
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
