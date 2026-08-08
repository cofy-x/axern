//go:build linux

package hostlinux

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestQuotaBoundaryErrorAcceptsKernelQuotaResults(t *testing.T) {
	for _, err := range []error{unix.EDQUOT, unix.ENOSPC, errors.Join(errors.New("write failed"), unix.EDQUOT)} {
		if !quotaBoundaryError(err) {
			t.Fatalf("quotaBoundaryError(%v) = false, want true", err)
		}
	}
	if quotaBoundaryError(unix.EIO) {
		t.Fatal("quotaBoundaryError(EIO) = true, want false")
	}
}
