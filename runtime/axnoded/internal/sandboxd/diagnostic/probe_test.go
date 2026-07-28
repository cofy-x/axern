package diagnostic

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestProbeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	host, port := serverHostPort(t, server.Listener.Addr().String())

	response := Probe(ProbeRequest{
		Host: host,
		HTTP: &HTTPProbe{
			Port: port,
			Path: "/ready",
		},
	})
	if !response.OK || response.Kind != "http" || !strings.Contains(response.Target, "/ready") {
		t.Fatalf("response = %#v", response)
	}
}

func TestProbeTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, port := serverHostPort(t, listener.Addr().String())

	response := Probe(ProbeRequest{Host: host, TCP: &TCPProbe{Port: port}})
	if !response.OK || response.Kind != "tcp" {
		t.Fatalf("response = %#v", response)
	}
}

func serverHostPort(t *testing.T, address string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
