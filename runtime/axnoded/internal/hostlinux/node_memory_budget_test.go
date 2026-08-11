package hostlinux

import "testing"

func TestPublicDelegatedRootLimitDoesNotLeakKernelMaxSentinel(t *testing.T) {
	if limit, finite := publicDelegatedRootLimit(-1); limit != 0 || finite {
		t.Fatalf("unbounded root = limit %d finite %t, want 0 false", limit, finite)
	}
	if limit, finite := publicDelegatedRootLimit(4096); limit != 4096 || !finite {
		t.Fatalf("finite root = limit %d finite %t, want 4096 true", limit, finite)
	}
}
