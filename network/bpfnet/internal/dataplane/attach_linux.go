//go:build linux

package dataplane

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"

	"github.com/cilium/ebpf"
	"github.com/cofy-x/axern/network/bpfnet/internal/tcprog"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type interfaceData struct {
	localAddresses []string
	localAddrs     map[tcprog.DataplaneLocalAddrKey]tcprog.DataplaneLocalAddrValue
	uplinkAddrs    map[tcprog.DataplaneUplinkAddrKey]tcprog.DataplaneUplinkAddrValue
}

func collectInterfaceData(uplinks []string) (interfaceData, error) {
	localAddrs := make(map[tcprog.DataplaneLocalAddrKey]tcprog.DataplaneLocalAddrValue)
	uplinkAddrs := make(map[tcprog.DataplaneUplinkAddrKey]tcprog.DataplaneUplinkAddrValue)
	pretty := make(map[string]struct{})

	loopback := net.IPv4(127, 0, 0, 1)
	localAddrs[tcprog.DataplaneLocalAddrKey{Addr: binary.BigEndian.Uint32(loopback.To4())}] = tcprog.DataplaneLocalAddrValue{Present: 1}
	pretty[loopback.String()] = struct{}{}

	for _, uplink := range uplinks {
		link, err := netlink.LinkByName(uplink)
		if err != nil {
			return interfaceData{}, fmt.Errorf("lookup uplink %s: %w", uplink, err)
		}
		iface, err := net.InterfaceByName(uplink)
		if err != nil {
			return interfaceData{}, fmt.Errorf("lookup uplink %s: %w", uplink, err)
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return interfaceData{}, fmt.Errorf("list addresses on %s: %w", uplink, err)
		}
		haveUplinkIPv4 := false
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ipv4 := ipnet.IP.To4()
			if ipv4 == nil {
				continue
			}
			key := tcprog.DataplaneLocalAddrKey{Addr: binary.BigEndian.Uint32(ipv4)}
			localAddrs[key] = tcprog.DataplaneLocalAddrValue{Present: 1}
			pretty[ipv4.String()] = struct{}{}
			if !haveUplinkIPv4 {
				uplinkAddrs[tcprog.DataplaneUplinkAddrKey{Ifindex: uint32(link.Attrs().Index)}] = tcprog.DataplaneUplinkAddrValue{
					Addr: binary.BigEndian.Uint32(ipv4),
				}
				haveUplinkIPv4 = true
			}
		}
		if !haveUplinkIPv4 {
			return interfaceData{}, fmt.Errorf("uplink %s has no ipv4 address", uplink)
		}
	}

	addresses := make([]string, 0, len(pretty))
	for addr := range pretty {
		addresses = append(addresses, addr)
	}
	sort.Strings(addresses)

	return interfaceData{
		localAddresses: addresses,
		localAddrs:     localAddrs,
		uplinkAddrs:    uplinkAddrs,
	}, nil
}

func attachTCProgram(device string, program *ebpf.Program, parent, handle uint32, name string) error {
	link, err := netlink.LinkByName(device)
	if err != nil {
		return fmt.Errorf("resolve uplink %s: %w", device, err)
	}

	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}
	if err := netlink.QdiscReplace(qdisc); err != nil {
		return fmt.Errorf("attach clsact to %s: %w", device, err)
	}

	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    parent,
			Handle:    handle,
			Priority:  1,
			Protocol:  unix.ETH_P_ALL,
		},
		Fd:           program.FD(),
		Name:         name,
		DirectAction: true,
	}
	if err := netlink.FilterReplace(filter); err != nil {
		return fmt.Errorf("attach tc filter on %s: %w", device, err)
	}
	return nil
}
