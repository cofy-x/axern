//go:build !linux

package netns

import (
	"fmt"
	"net"
)

func ListenTCPInPath(path, address string) (net.Listener, error) {
	return nil, fmt.Errorf("network namespace listening is only supported on linux: %s %s", path, address)
}
