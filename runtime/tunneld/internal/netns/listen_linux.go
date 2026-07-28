//go:build linux

package netns

import (
	"net"
	"runtime"

	vnetns "github.com/vishvananda/netns"
)

func ListenTCPInPath(path, address string) (net.Listener, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	orig, err := vnetns.Get()
	if err != nil {
		return nil, err
	}
	defer orig.Close()
	target, err := vnetns.GetFromPath(path)
	if err != nil {
		return nil, err
	}
	defer target.Close()
	if err := vnetns.Set(target); err != nil {
		return nil, err
	}
	defer vnetns.Set(orig)
	return net.Listen("tcp", address)
}
