package ocicli

import (
	"errors"
	"testing"
)

func TestIsContainerNotFound(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		id     string
		absent bool
	}{
		{name: "runc missing container", err: &CommandError{Err: errors.New("exit status 1"), Output: `container "sandbox" does not exist`}, id: "sandbox", absent: true},
		{name: "runc scoped missing container", err: &CommandError{Err: errors.New("exit status 1"), Output: `time="2026-08-12T17:18:02Z" level=error msg="container does not exist"`}, id: "sandbox", absent: true},
		{name: "runc plain scoped missing container", err: &CommandError{Err: errors.New("exit status 1"), Output: "container does not exist"}, id: "sandbox", absent: true},
		{name: "runsc missing container", err: &CommandError{Err: errors.New("exit status 128"), Output: "error: container sandbox not found"}, id: "sandbox", absent: true},
		{name: "generic no such container", err: &CommandError{Err: errors.New("exit status 1"), Output: "no such container: sandbox"}, id: "sandbox", absent: true},
		{name: "different container", err: &CommandError{Err: errors.New("exit status 1"), Output: "container other does not exist"}, id: "sandbox", absent: false},
		{name: "container rootfs missing", err: &CommandError{Err: errors.New("exit status 1"), Output: "container sandbox rootfs not found"}, id: "sandbox", absent: false},
		{name: "unscoped phrase in diagnostic", err: &CommandError{Err: errors.New("exit status 1"), Output: "rootfs check: container does not exist"}, id: "sandbox", absent: false},
		{name: "extended structured message", err: &CommandError{Err: errors.New("exit status 1"), Output: `time="now" level=error msg="container does not exist while loading rootfs"`}, id: "sandbox", absent: false},
		{name: "missing runtime binary", err: &CommandError{Err: errors.New("executable file not found"), Output: ""}, id: "sandbox", absent: false},
		{name: "missing bundle", err: &CommandError{Err: errors.New("exit status 1"), Output: "bundle path not found"}, id: "sandbox", absent: false},
		{name: "unstructured error", err: errors.New("container sandbox does not exist"), id: "sandbox", absent: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsContainerNotFound(test.err, test.id); got != test.absent {
				t.Fatalf("IsContainerNotFound() = %v, want %v", got, test.absent)
			}
		})
	}
}

func TestCommandErrorUnwrapsProcessError(t *testing.T) {
	processErr := errors.New("exit status 1")
	err := &CommandError{Err: processErr, Output: "runtime failure"}
	if !errors.Is(err, processErr) {
		t.Fatal("CommandError must preserve the underlying process error")
	}
	if got := err.Error(); got != "exit status 1: runtime failure" {
		t.Fatalf("CommandError.Error() = %q", got)
	}
}
