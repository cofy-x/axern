package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestRawTCPFixtureServesMultipleRequestsPerConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	go serveRawTCP(ctx, listener)
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	reader := bufio.NewReader(connection)
	for request := 0; request < 2; request++ {
		if _, err := io.WriteString(connection, "probe\n"); err != nil {
			t.Fatal(err)
		}
		response, err := reader.ReadString('\n')
		if err != nil || response != "ok\n" {
			t.Fatalf("response %d = %q, err = %v", request, response, err)
		}
	}
}

func TestDNSResponseUsesRequestedAddressFamily(t *testing.T) {
	for _, test := range []struct {
		name       string
		answer     net.IP
		queryType  dnsmessage.Type
		answerType dnsmessage.Type
	}{
		{name: "ipv4", answer: net.ParseIP("192.0.2.10"), queryType: dnsmessage.TypeA, answerType: dnsmessage.TypeA},
		{name: "ipv6", answer: net.ParseIP("2001:db8::10"), queryType: dnsmessage.TypeAAAA, answerType: dnsmessage.TypeAAAA},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := dnsmessage.Message{Header: dnsmessage.Header{ID: 7}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(allowedName), Type: test.queryType, Class: dnsmessage.ClassINET}}}
			wire, err := query.Pack()
			if err != nil {
				t.Fatal(err)
			}
			responseWire, err := dnsResponse(wire, test.answer)
			if err != nil {
				t.Fatal(err)
			}
			var response dnsmessage.Message
			if err := response.Unpack(responseWire); err != nil {
				t.Fatal(err)
			}
			if response.Header.RCode != dnsmessage.RCodeSuccess || len(response.Answers) != 1 || response.Answers[0].Header.Type != test.answerType {
				t.Fatalf("unexpected DNS response: %#v", response)
			}
		})
	}
}

func TestServePayloadBoundsAndHost(t *testing.T) {
	request := httptest.NewRequest("GET", "http://allowed.fixture.axern.test/payload?bytes=17", nil)
	request.Host = "allowed.fixture.axern.test"
	response := httptest.NewRecorder()
	servePayload(response, request)
	if response.Code != 200 || response.Body.Len() != 17 || response.Header().Get("Content-Length") != "17" {
		t.Fatalf("unexpected payload response: code=%d bytes=%d headers=%v", response.Code, response.Body.Len(), response.Header())
	}

	request = httptest.NewRequest("GET", "http://denied.fixture.axern.test/payload?bytes=17", nil)
	request.Host = "denied.fixture.axern.test"
	response = httptest.NewRecorder()
	servePayload(response, request)
	if response.Code != 421 {
		t.Fatalf("unexpected denied host status %d", response.Code)
	}
}
