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
