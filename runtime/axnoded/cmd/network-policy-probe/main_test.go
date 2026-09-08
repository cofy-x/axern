package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestSustainedHTTPReusesConnections(t *testing.T) {
	var connections, requests atomic.Uint64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		response.Header().Set("Content-Length", "1")
		_, _ = response.Write([]byte{0})
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	address := server.Listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var operations, failures atomic.Uint64
	sustainedHTTPAt(ctx, config{host: "allowed.fixture.axern.test", timeout: time.Second}, address, &operations, &failures)
	if operations.Load() < 2 || failures.Load() != 0 || requests.Load() < operations.Load() || connections.Load() != 1 {
		t.Fatalf("operations=%d failures=%d requests=%d connections=%d", operations.Load(), failures.Load(), requests.Load(), connections.Load())
	}
}

func TestSustainedRawTCPReusesOneConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var accepts atomic.Uint64
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		accepts.Add(1)
		defer connection.Close()
		reader := bufio.NewReader(connection)
		for {
			if _, readErr := reader.ReadString('\n'); readErr != nil {
				return
			}
			if _, writeErr := io.WriteString(connection, "ok\n"); writeErr != nil {
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var operations, failures atomic.Uint64
	sustainedRawTCP(ctx, listener.Addr().String(), time.Second, &operations, &failures)
	if operations.Load() < 2 || failures.Load() != 0 || accepts.Load() != 1 {
		t.Fatalf("operations=%d failures=%d accepts=%d", operations.Load(), failures.Load(), accepts.Load())
	}
}

func TestConfigValidationIsModeAware(t *testing.T) {
	base := config{mode: "strict_cidr", address: "192.0.2.10", payloadBytes: 1, concurrency: 1, sustainedTime: time.Second, timeout: time.Second}
	if err := base.validate(); err != nil {
		t.Fatalf("strict CIDR config rejected: %v", err)
	}
	base.mode = "strict_domain"
	if err := base.validate(); err == nil {
		t.Fatal("strict domain config accepted without DNS fixture")
	}
	base.dnsServer = "192.0.2.53"
	if err := base.validate(); err != nil {
		t.Fatalf("strict domain config rejected: %v", err)
	}
}

func TestQueryDNSAcceptsExplicitFixturePort(t *testing.T) {
	connection, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	go func() {
		buffer := make([]byte, 65535)
		n, peer, readErr := connection.ReadFrom(buffer)
		if readErr != nil {
			return
		}
		var query dnsmessage.Message
		if query.Unpack(buffer[:n]) != nil {
			return
		}
		answer := dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true}, Questions: query.Questions, Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 30}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 10}}}}}
		wire, packErr := answer.Pack()
		if packErr == nil {
			_, _ = connection.WriteTo(wire, peer)
		}
	}()
	if err := queryDNS(connection.LocalAddr().String(), allowedFixtureName, dnsmessage.TypeA, dnsmessage.RCodeSuccess, time.Second); err != nil {
		t.Fatal(err)
	}
}
