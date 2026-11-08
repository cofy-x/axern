//go:build !linux

package proc

import (
	"fmt"
	"io"
	"os/exec"
)

func PrepareUnprivilegedCommand(*exec.Cmd, uint32, uint32) (func(), error) {
	return nil, fmt.Errorf("unprivileged sandbox execution requires Linux capability controls")
}

func RunInternalExec(_ []string, stderr io.Writer) int {
	fmt.Fprintln(stderr, "axern-sandboxd: unprivileged sandbox execution requires Linux capability controls")
	return 126
}
