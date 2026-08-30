package main

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestResponseForDeterministicRecords(t *testing.T) {
	tests := []struct {
		name        string
		queryName   string
		kind        dnsmessage.Type
		rcode       dnsmessage.RCode
		answerCount int
	}{
		{"a", fixtureName, dnsmessage.TypeA, dnsmessage.RCodeSuccess, 1},
		{"aaaa", fixtureName, dnsmessage.TypeAAAA, dnsmessage.RCodeSuccess, 1},
		{"alias", fixtureAlias, dnsmessage.TypeA, dnsmessage.RCodeSuccess, 2},
		{"refused", fixtureRefused, dnsmessage.TypeA, dnsmessage.RCodeRefused, 0},
		{"unknown", "unknown.axern.test.", dnsmessage.TypeA, dnsmessage.RCodeNameError, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire, err := responseFor(queryWire(t, test.queryName, test.kind))
			if err != nil {
				t.Fatal(err)
			}
			var response dnsmessage.Message
			if err := response.Unpack(wire); err != nil {
				t.Fatal(err)
			}
			if response.Header.ID != 7 || !response.Header.Response || !response.Header.Authoritative || response.Header.RCode != test.rcode {
				t.Fatalf("unexpected response header: %#v", response.Header)
			}
			if len(response.Answers) != test.answerCount {
				t.Fatalf("answer count = %d, want %d", len(response.Answers), test.answerCount)
			}
			for _, answer := range response.Answers {
				if answer.Header.TTL != fixtureTTL {
					t.Fatalf("TTL = %d, want %d", answer.Header.TTL, fixtureTTL)
				}
			}
		})
	}
}

func TestResponseForRejectsMultipleQuestions(t *testing.T) {
	name := dnsmessage.MustNewName(fixtureName)
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: 7}, Questions: []dnsmessage.Question{
		{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		{Name: name, Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET},
	}}
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	wire, err = responseFor(wire)
	if err != nil {
		t.Fatal(err)
	}
	var response dnsmessage.Message
	if err := response.Unpack(wire); err != nil {
		t.Fatal(err)
	}
	if response.Header.RCode != dnsmessage.RCodeFormatError {
		t.Fatalf("rcode = %v, want format error", response.Header.RCode)
	}
}

func TestCheckUsesUDPAndTCP(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		_ = tcp.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer tcp.Close()
	defer udp.Close()
	go func() { _ = serveUDP(ctx, udp) }()
	go func() { _ = serveTCP(ctx, tcp) }()
	checkCtx, checkCancel := context.WithTimeout(context.Background(), time.Second)
	defer checkCancel()
	if err := check(checkCtx, tcp.Addr().String()); err != nil {
		t.Fatal(err)
	}
}

func queryWire(t *testing.T, name string, kind dnsmessage.Type) []byte {
	t.Helper()
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: 7, RecursionDesired: true}, Questions: []dnsmessage.Question{{
		Name: dnsmessage.MustNewName(name), Type: kind, Class: dnsmessage.ClassINET,
	}}}
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
