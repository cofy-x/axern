//go:build !linux

package allocation

import (
	"fmt"
	"os"
)

func prepareWorkspaceCOW(_, _, _ string) (string, func(), error) {
	return "", nil, fmt.Errorf("copy-on-write workspace is only supported on linux")
}

func cleanupWorkspaceCOW(root string) error { return os.RemoveAll(root) }

func restoreWorkspaceCOW(_, _ string) (string, error) {
	return "", fmt.Errorf("copy-on-write workspace recovery is only supported on linux")
}
