package oci

import "testing"

func TestImageLock_SameImageSerializes(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	assertSameKeySerializes(t, mgr.acquireImageLock, "docker.io/library/alpine:latest")
}

func TestImageLock_DifferentImagesCanRunConcurrently(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	assertDifferentKeysRunConcurrently(t, mgr.acquireImageLock, "docker.io/library/alpine:latest", "docker.io/library/busybox:latest")
}

func TestLayerLock_SameLayerSerializes(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	assertSameKeySerializes(t, mgr.acquireLayerLock, "sha256:same")
}

func TestLayerLock_DifferentLayersCanRunConcurrently(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	assertDifferentKeysRunConcurrently(t, mgr.acquireLayerLock, "sha256:a", "sha256:b")
}
