package main

import (
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

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
