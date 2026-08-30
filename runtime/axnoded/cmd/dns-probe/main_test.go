package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestEffectiveResolversLoadsAxnodedRuntimeConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "axnoded.toml")
	if err := os.WriteFile(path, []byte("[plugin.runtime.dns]\nnameservers = [\"192.0.2.53\", \"2001:db8::53\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := effectiveResolvers(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "192.0.2.53,2001:db8::53" {
		t.Fatalf("effectiveResolvers() = %v", got)
	}
}

func TestParseResolversValidatesDeduplicatesAndCanonicalizes(t *testing.T) {
	got := parseResolvers("192.0.2.53, 2001:db8::53, ::ffff:192.0.2.53, 127.0.0.1, ::, invalid")
	want := []string{"192.0.2.53", "2001:db8::53"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parseResolvers() = %v, want %v", got, want)
	}
}

func TestRunProbeClassifiesAllPartialAndNoSuccess(t *testing.T) {
	tests := []struct {
		name       string
		successful string
		status     string
		code       string
		count      int64
	}{
		{"all", "192.0.2.53,2001:db8::53", "pass", "runtime_dns_node_reachable", 2},
		{"partial", "192.0.2.53", "warn", "runtime_dns_node_partial", 1},
		{"none", "", "fail", "runtime_dns_node_unreachable", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := runProbe(context.Background(), []string{"192.0.2.53", "2001:db8::53"}, "example.test.", time.Second, func(_ context.Context, address, _ string, _ time.Duration) bool {
				host, _, err := net.SplitHostPort(address)
				return err == nil && strings.Contains(test.successful, host)
			})
			if value.Status != test.status || value.Code != test.code || value.SuccessfulResolverCount != test.count {
				t.Fatalf("runProbe() = %#v", value)
			}
		})
	}
}

func TestRunProbeCancellationAndOutputAreSanitized(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value := runProbe(ctx, []string{"192.0.2.53"}, "private.corp.example.", time.Second, func(ctx context.Context, _, _ string, _ time.Duration) bool {
		return ctx.Err() == nil
	})
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "192.0.2.53") || strings.Contains(text, "private.corp.example") {
		t.Fatalf("probe output leaked sensitive input: %s", text)
	}
	if value.Code != "runtime_dns_node_unreachable" {
		t.Fatalf("unexpected cancellation result: %#v", value)
	}
}

func TestRunProbeBoundsResolverTimeout(t *testing.T) {
	started := time.Now()
	value := runProbe(context.Background(), []string{"192.0.2.53"}, "example.test.", 20*time.Millisecond, func(ctx context.Context, _, _ string, _ time.Duration) bool {
		<-ctx.Done()
		return false
	})
	if value.Code != "runtime_dns_node_unreachable" || time.Since(started) > time.Second {
		t.Fatalf("timeout result = %#v after %s", value, time.Since(started))
	}
}

func TestValidQueryName(t *testing.T) {
	for _, value := range []string{"example.test.", "a-b.example", "AXERN.COFY-X.SPACE."} {
		if !validQueryName(value) {
			t.Fatalf("validQueryName(%q) = false", value)
		}
	}
	for _, value := range []string{"", "-bad.example", "bad_.example", "bad..example"} {
		if validQueryName(value) {
			t.Fatalf("validQueryName(%q) = true", value)
		}
	}
}

func TestLookupResolverSupportsUDPAndTCPFallback(t *testing.T) {
	for _, truncateUDP := range []bool{false, true} {
		t.Run(map[bool]string{false: "udp", true: "tcp_fallback"}[truncateUDP], func(t *testing.T) {
			address, closeFixture := startDNSFixture(t, truncateUDP)
			defer closeFixture()
			if !lookupResolver(context.Background(), address, "example.test.", time.Second) {
				t.Fatal("lookupResolver() = false")
			}
		})
	}
}

func startDNSFixture(t *testing.T, truncateUDP bool) (string, func()) {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go serveUDPFixture(udp, truncateUDP, closed)
	go serveTCPFixture(tcp, closed)
	return tcp.Addr().String(), func() {
		close(closed)
		_ = udp.Close()
		_ = tcp.Close()
	}
}

func serveUDPFixture(connection net.PacketConn, truncated bool, closed <-chan struct{}) {
	buffer := make([]byte, 2048)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, address, err := connection.ReadFrom(buffer)
		if err != nil {
			select {
			case <-closed:
				return
			default:
				continue
			}
		}
		if response, ok := dnsResponse(buffer[:n], truncated); ok {
			_, _ = connection.WriteTo(response, address)
		}
	}
}

func serveTCPFixture(listener net.Listener, closed <-chan struct{}) {
	for {
		_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
		connection, err := listener.Accept()
		if err != nil {
			select {
			case <-closed:
				return
			default:
				continue
			}
		}
		go func() {
			defer connection.Close()
			var size [2]byte
			if _, err := io.ReadFull(connection, size[:]); err != nil {
				return
			}
			request := make([]byte, binary.BigEndian.Uint16(size[:]))
			if _, err := io.ReadFull(connection, request); err != nil {
				return
			}
			response, ok := dnsResponse(request, false)
			if !ok {
				return
			}
			binary.BigEndian.PutUint16(size[:], uint16(len(response)))
			_, _ = connection.Write(append(size[:], response...))
		}()
	}
}

func dnsResponse(request []byte, truncated bool) ([]byte, bool) {
	var parser dnsmessage.Parser
	header, err := parser.Start(request)
	if err != nil {
		return nil, false
	}
	question, err := parser.Question()
	if err != nil {
		return nil, false
	}
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: header.ID, Response: true, Authoritative: true, Truncated: truncated})
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		return nil, false
	}
	if err := builder.Question(question); err != nil {
		return nil, false
	}
	if truncated {
		response, err := builder.Finish()
		return response, err == nil
	}
	if err := builder.StartAnswers(); err != nil {
		return nil, false
	}
	resourceHeader := dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 30}
	switch question.Type {
	case dnsmessage.TypeA:
		err = builder.AResource(resourceHeader, dnsmessage.AResource{A: [4]byte{192, 0, 2, 10}})
	case dnsmessage.TypeAAAA:
		err = builder.AAAAResource(resourceHeader, dnsmessage.AAAAResource{AAAA: [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 10}})
	default:
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	response, err := builder.Finish()
	return response, err == nil
}
