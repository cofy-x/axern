//go:build linux

package cgroup

import (
	"errors"
	"syscall"
	"testing"
)

func TestEnableControllersAfterEvacuationConvergesAcrossBusyRace(t *testing.T) {
	moves := 0
	enables := 0
	err := enableControllersAfterEvacuation(
		func() error {
			moves++
			return nil
		},
		func() error {
			enables++
			if enables < 4 {
				return syscall.EBUSY
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if moves != 4 || enables != 4 {
		t.Fatalf("moves=%d enables=%d, want 4 each", moves, enables)
	}
}

func TestEnableControllersAfterEvacuationFailsClosed(t *testing.T) {
	want := errors.New("permission denied")
	if err := enableControllersAfterEvacuation(func() error { return nil }, func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("non-busy error = %v, want %v", err, want)
	}

	moves := 0
	err := enableControllersAfterEvacuation(
		func() error {
			moves++
			return nil
		},
		func() error { return syscall.EBUSY },
	)
	if !errors.Is(err, syscall.EBUSY) || moves != controllerEnableAttempts {
		t.Fatalf("persistent busy error=%v moves=%d, want EBUSY after %d attempts", err, moves, controllerEnableAttempts)
	}
}
