package diagnostic

import "testing"

func TestParseProcNetLocalAddressIPv4(t *testing.T) {
	address, port, ok := parseProcNetLocalAddress("0100007F:46A1", false)
	if !ok {
		t.Fatal("parseProcNetLocalAddress() ok = false")
	}
	if address != "127.0.0.1" || port != 18081 {
		t.Fatalf("address=%q port=%d, want 127.0.0.1:18081", address, port)
	}
}

func TestParseProcNetLocalAddressIPv6(t *testing.T) {
	address, port, ok := parseProcNetLocalAddress("00000000000000000000000000000000:0050", true)
	if !ok {
		t.Fatal("parseProcNetLocalAddress() ok = false")
	}
	if address != "00000000000000000000000000000000" || port != 80 {
		t.Fatalf("address=%q port=%d, want raw ipv6 and port 80", address, port)
	}
}
