package oci

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/rootfssupport"
)

type panicUncompressedLayer struct {
	digest v1.Hash
}

func (l panicUncompressedLayer) Digest() (v1.Hash, error) { return l.digest, nil }
func (l panicUncompressedLayer) DiffID() (v1.Hash, error) { return l.digest, nil }
func (l panicUncompressedLayer) Compressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (l panicUncompressedLayer) Uncompressed() (io.ReadCloser, error) {
	return nil, fmt.Errorf("unexpected uncompressed call")
}
func (l panicUncompressedLayer) Size() (int64, error) { return 0, nil }
func (l panicUncompressedLayer) MediaType() (types.MediaType, error) {
	return types.OCIUncompressedLayer, nil
}

type sleepLayer struct {
	digest v1.Hash
	delay  time.Duration
}

func (l sleepLayer) Digest() (v1.Hash, error) { return l.digest, nil }
func (l sleepLayer) DiffID() (v1.Hash, error) { return l.digest, nil }
func (l sleepLayer) Compressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (l sleepLayer) Uncompressed() (io.ReadCloser, error) {
	time.Sleep(l.delay)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("layer-data")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "etc/config",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}
func (l sleepLayer) Size() (int64, error)                { return 0, nil }
func (l sleepLayer) MediaType() (types.MediaType, error) { return types.OCIUncompressedLayer, nil }

type observedSleepLayer struct {
	digest    v1.Hash
	delay     time.Duration
	inFlight  *int32
	maxFlight *int32
}

func (l observedSleepLayer) Digest() (v1.Hash, error) { return l.digest, nil }
func (l observedSleepLayer) DiffID() (v1.Hash, error) { return l.digest, nil }
func (l observedSleepLayer) Compressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (l observedSleepLayer) Uncompressed() (io.ReadCloser, error) {
	cur := atomic.AddInt32(l.inFlight, 1)
	for {
		prev := atomic.LoadInt32(l.maxFlight)
		if cur <= prev || atomic.CompareAndSwapInt32(l.maxFlight, prev, cur) {
			break
		}
	}
	defer atomic.AddInt32(l.inFlight, -1)

	time.Sleep(l.delay)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("layer-data")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "etc/config",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}
func (l observedSleepLayer) Size() (int64, error) { return 0, nil }
func (l observedSleepLayer) MediaType() (types.MediaType, error) {
	return types.OCIUncompressedLayer, nil
}

type blockLayer struct {
	digest  v1.Hash
	started chan<- struct{}
	unblock <-chan struct{}
}

func (l blockLayer) Digest() (v1.Hash, error) { return l.digest, nil }
func (l blockLayer) DiffID() (v1.Hash, error) { return l.digest, nil }
func (l blockLayer) Compressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (l blockLayer) Uncompressed() (io.ReadCloser, error) {
	select {
	case l.started <- struct{}{}:
	default:
	}
	<-l.unblock

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("layer-data")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "etc/config",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}
func (l blockLayer) Size() (int64, error)                { return 0, nil }
func (l blockLayer) MediaType() (types.MediaType, error) { return types.OCIUncompressedLayer, nil }

type errorLayer struct {
	digest v1.Hash
	err    error
}

func (l errorLayer) Digest() (v1.Hash, error) { return l.digest, nil }
func (l errorLayer) DiffID() (v1.Hash, error) { return l.digest, nil }
func (l errorLayer) Compressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (l errorLayer) Uncompressed() (io.ReadCloser, error) {
	if l.err != nil {
		return nil, l.err
	}
	return nil, fmt.Errorf("error layer")
}
func (l errorLayer) Size() (int64, error)                { return 0, nil }
func (l errorLayer) MediaType() (types.MediaType, error) { return types.OCIUncompressedLayer, nil }

func assertSameKeySerializes(t *testing.T, acquire func(string) func(), key string) {
	t.Helper()

	firstLocked := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondTrying := make(chan struct{})
	secondLocked := make(chan struct{})
	done := make(chan struct{}, 2)

	go func() {
		unlock := acquire(key)
		close(firstLocked)
		<-releaseFirst
		unlock()
		done <- struct{}{}
	}()

	waitForSignal(t, firstLocked, "first lock was not acquired")

	go func() {
		close(secondTrying)
		unlock := acquire(key)
		close(secondLocked)
		unlock()
		done <- struct{}{}
	}()

	waitForSignal(t, secondTrying, "second lock attempt did not start")
	assertNotSignaled(t, secondLocked, "same key lock should serialize")

	close(releaseFirst)

	waitForSignal(t, secondLocked, "second lock did not acquire after the first lock was released")
	waitForSignal(t, done, "first lock holder did not finish")
	waitForSignal(t, done, "second lock holder did not finish")
}

func assertDifferentKeysRunConcurrently(t *testing.T, acquire func(string) func(), firstKey string, secondKey string) {
	t.Helper()

	firstLocked := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondLocked := make(chan struct{})
	releaseSecond := make(chan struct{})
	done := make(chan struct{}, 2)

	go func() {
		unlock := acquire(firstKey)
		close(firstLocked)
		<-releaseFirst
		unlock()
		done <- struct{}{}
	}()

	waitForSignal(t, firstLocked, "first lock was not acquired")

	go func() {
		unlock := acquire(secondKey)
		close(secondLocked)
		<-releaseSecond
		unlock()
		done <- struct{}{}
	}()

	waitForSignal(t, secondLocked, "different keys should acquire locks independently")

	close(releaseSecond)
	close(releaseFirst)

	waitForSignal(t, done, "first lock holder did not finish")
	waitForSignal(t, done, "second lock holder did not finish")
}

func waitForSignal(t *testing.T, ch <-chan struct{}, failureMsg string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal(failureMsg)
	}
}

func assertNotSignaled(t *testing.T, ch <-chan struct{}, failureMsg string) {
	t.Helper()

	select {
	case <-ch:
		t.Fatal(failureMsg)
	default:
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	root := t.TempDir()
	layersDir := filepath.Join(root, "layers")
	chainsDir := filepath.Join(root, "lowerdirs")
	mountsDir := filepath.Join(root, "mounts")
	importsDir := filepath.Join(root, "imports")
	supportDir := filepath.Join(root, "support", "fs")
	if err := os.MkdirAll(layersDir, 0755); err != nil {
		t.Fatalf("mkdir layers dir: %v", err)
	}
	if err := os.MkdirAll(chainsDir, 0755); err != nil {
		t.Fatalf("mkdir lowerdirs dir: %v", err)
	}
	if err := os.MkdirAll(mountsDir, 0755); err != nil {
		t.Fatalf("mkdir mounts dir: %v", err)
	}
	if err := os.MkdirAll(importsDir, 0755); err != nil {
		t.Fatalf("mkdir imports dir: %v", err)
	}
	if err := rootfssupport.Ensure(supportDir); err != nil {
		t.Fatalf("mkdir support dir: %v", err)
	}

	store, err := openMetadataStore(filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}

	mgr := &Manager{
		root:       root,
		layersDir:  layersDir,
		chainsDir:  chainsDir,
		mountsDir:  mountsDir,
		importsDir: importsDir,
		supportDir: supportDir,
		store:      store,
		containers: make(map[string]*ContainerInfo),
		imageLocks: make(map[string]*imageLockEntry),
		layerLocks: make(map[string]*imageLockEntry),
		chainLocks: make(map[string]*imageLockEntry),
		stopCh:     make(chan struct{}),
		now:        func() time.Time { return time.Unix(1700000000, 0) },
		mountFn:    func(string, []string) error { return nil },
		unmountFn:  func(string) error { return nil },
		readMnts: func() (managedMountSnapshot, error) {
			return managedMountSnapshot{paths: map[string]struct{}{}}, nil
		},
		diskUsage:    func(string) (float64, error) { return 0, nil },
		layerWorkers: defaultGlobalLayerWorkers,
		layerTTL:     defaultLayerZeroRefTTL,
	}
	t.Cleanup(func() {
		mgr.layerPoolMu.Lock()
		mgr.stopOnce.Do(func() {
			close(mgr.stopCh)
		})
		mgr.layerPoolWG.Wait()
		mgr.layerPoolMu.Unlock()
	})
	return mgr
}
