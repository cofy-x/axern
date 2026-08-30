package l7inspect

import (
	"strings"
	"testing"
)

func TestReadHTTPRequestCanonicalizesHostAndPreservesHeader(t *testing.T) {
	wire := "GET /path HTTP/1.1\r\nHost: BÜCHER.Example:80\r\nUser-Agent: test\r\n\r\nbody"
	request, err := ReadHTTPRequest(strings.NewReader(wire), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != "GET" || request.Host != "xn--bcher-kva.example" || request.DirectIP {
		t.Fatalf("unexpected request: %#v", request)
	}
	if string(request.Bytes) != strings.Split(wire, "body")[0] {
		t.Fatalf("unexpected preserved header: %q", request.Bytes)
	}
}

func TestReadHTTPRequestClassifiesDirectIPAndRejectsCONNECT(t *testing.T) {
	request, err := ReadHTTPRequest(strings.NewReader("GET / HTTP/1.1\r\nHost: 192.0.2.1\r\n\r\n"), 4096)
	if err != nil || !request.DirectIP || request.Host != "192.0.2.1" {
		t.Fatalf("direct IP request = (%#v, %v)", request, err)
	}
	if _, err := ReadHTTPRequest(strings.NewReader("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"), 4096); err == nil {
		t.Fatal("CONNECT was accepted")
	}
}

func TestReadHTTPRequestEnforcesHeaderBound(t *testing.T) {
	wire := "GET / HTTP/1.1\r\nHost: example.com\r\nX-Large: " + strings.Repeat("x", 256) + "\r\n\r\n"
	if _, err := ReadHTTPRequest(strings.NewReader(wire), 64); err == nil {
		t.Fatal("oversized HTTP header was accepted")
	}
	if _, err := ReadHTTPRequest(strings.NewReader("GET / HTTP/1.1\r\n\r\n"), 4096); err == nil {
		t.Fatal("missing Host was accepted")
	}
	if _, err := ReadHTTPRequest(strings.NewReader("GET / HTTP/1.1\r\nHost: example.com:https\r\n\r\n"), 4096); err == nil {
		t.Fatal("non-numeric Host port was accepted")
	}
}

func TestReadHTTPRequestRejectsAmbiguousAuthorityForms(t *testing.T) {
	for _, wire := range []string{
		"GET http://allowed.example/ HTTP/1.1\r\nHost: denied.example\r\n\r\n",
		"GET / HTTP/1.1\r\nHost: allowed.example\r\nHost: denied.example\r\n\r\n",
		"GET / HTTP/1.1\r\nHost: allowed.example:443\r\n\r\n",
	} {
		if request, err := ReadHTTPRequest(strings.NewReader(wire), 4096); err == nil {
			t.Fatalf("ambiguous HTTP authority was accepted: %#v", request)
		}
	}
}
