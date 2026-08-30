package resources

import (
	"net"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestIPToVeth(t *testing.T) {
	gotHost, gotPeer := ipToVeth("172.17.0.24")
	if gotHost != config.HostVethPrefix+"ac110018" {
		t.Fatalf("ipToVeth() host = %q", gotHost)
	}
	if gotPeer != config.PeerVethPrefix+"ac110018" {
		t.Fatalf("ipToVeth() peer = %q", gotPeer)
	}
}

func TestIPv6ToVethFitsLinuxInterfaceName(t *testing.T) {
	host, peer := ipToVeth("fd31::1234")
	if host != config.HostVethPrefix+"000000001234" || peer != config.PeerVethPrefix+"000000001234" {
		t.Fatalf("ipToVeth() = %q, %q", host, peer)
	}
	if len(host) > 15 || len(peer) > 15 {
		t.Fatalf("IPv6 veth names exceed IFNAMSIZ: %q, %q", host, peer)
	}
	if got := vethToIP(host, "fd31::1/64"); got.String() != "fd31::1234" {
		t.Fatalf("vethToIP() = %v", got)
	}
}

func TestVethToIP(t *testing.T) {
	tests := []struct {
		name string
		veth string
		want net.IP
	}{
		{"from host veth", config.HostVethPrefix + "ac110018", net.ParseIP("172.17.0.24")},
		{"from peer veth", config.PeerVethPrefix + "ac110018", net.ParseIP("172.17.0.24")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vethToIP(tt.veth); got.String() != tt.want.String() {
				t.Fatalf("vethToIP(%s) = %v, want %v", tt.veth, got, tt.want)
			}
		})
	}
}

func TestGenerateIP(t *testing.T) {
	gatewayIP, mask, ips, err := generateIP("172.17.0.1/16", 1000)
	if err != nil {
		t.Fatalf("generateIP() error = %v", err)
	}
	if gatewayIP == nil || mask == nil {
		t.Fatal("generateIP() returned nil gateway or mask")
	}
	for ip := range ips {
		if ip == gatewayIP.String() {
			t.Fatalf("generateIP() included gateway %s in pool", ip)
		}
	}
}

func TestGenerateIPv6(t *testing.T) {
	gatewayIP, mask, ips, err := generateIP("fd31::1/64", 1000)
	if err != nil {
		t.Fatalf("generateIP() error = %v", err)
	}
	if gatewayIP.String() != "fd31::1" {
		t.Fatalf("gateway = %s", gatewayIP)
	}
	ones, bits := mask.Size()
	if ones != 64 || bits != 128 {
		t.Fatalf("mask = %d/%d", ones, bits)
	}
	if _, ok := ips["fd31::2"]; !ok {
		t.Fatal("first IPv6 sandbox address is missing")
	}
}
