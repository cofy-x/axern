//go:build test
// +build test

package imagefsd

import "time"

func init() {
	// Reduce timeouts for faster tests
	daemonMountTimeout = 2 * time.Second
	daemonUnmountTimeout = 2 * time.Second
}
