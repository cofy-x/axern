package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	defaultListenAddress = "0.0.0.0:53"
	fixtureName          = "fixture.axern.test."
	fixtureAlias         = "alias.fixture.axern.test."
	fixtureRefused       = "refused.fixture.axern.test."
	fixtureTTL           = 30
)

var (
	fixtureA    = [4]byte{192, 0, 2, 10}
	fixtureAAAA = [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 10}
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "dns-fixture: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("dns-fixture", flag.ContinueOnError)
	listenAddress := flags.String("listen", defaultListenAddress, "UDP and TCP listen address")
	checkAddress := flags.String("check", "", "query an existing fixture over UDP and TCP")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if strings.TrimSpace(*checkAddress) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return check(ctx, strings.TrimSpace(*checkAddress))
	}
	return serve(strings.TrimSpace(*listenAddress))
}

func serve(address string) error {
	if address == "" {
		return errors.New("listen address is required")
	}
	tcp, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen TCP: %w", err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", address)
	if err != nil {
		return fmt.Errorf("listen UDP: %w", err)
	}
	defer udp.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- serveUDP(ctx, udp) }()
	go func() { errorsCh <- serveTCP(ctx, tcp) }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errorsCh:
		return err
	}
}

func serveUDP(ctx context.Context, connection net.PacketConn) error {
	buffer := make([]byte, 65535)
	for {
		if err := connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			return err
		}
		n, peer, err := connection.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return fmt.Errorf("read UDP query: %w", err)
		}
		response, err := responseFor(buffer[:n])
		if err != nil {
			continue
		}
		if _, err := connection.WriteTo(response, peer); err != nil {
			return fmt.Errorf("write UDP response: %w", err)
		}
	}
}

func serveTCP(ctx context.Context, listener net.Listener) error {
	tcp := listener.(*net.TCPListener)
	for {
		if err := tcp.SetDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			return err
		}
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return fmt.Errorf("accept TCP query: %w", err)
		}
		go handleTCP(connection)
	}
}

func handleTCP(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	var size [2]byte
	if _, err := io.ReadFull(connection, size[:]); err != nil {
		return
	}
	request := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(connection, request); err != nil {
		return
	}
	response, err := responseFor(request)
	if err != nil || len(response) > 65535 {
		return
	}
	binary.BigEndian.PutUint16(size[:], uint16(len(response)))
	_, _ = connection.Write(append(size[:], response...))
}

func responseFor(wire []byte) ([]byte, error) {
	var query dnsmessage.Message
	if err := query.Unpack(wire); err != nil {
		return nil, err
	}
	response := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 query.Header.ID,
			Response:           true,
			Authoritative:      true,
			RecursionDesired:   query.Header.RecursionDesired,
			RecursionAvailable: true,
		},
		Questions: append([]dnsmessage.Question(nil), query.Questions...),
	}
	if len(query.Questions) != 1 || query.Questions[0].Class != dnsmessage.ClassINET {
		response.Header.RCode = dnsmessage.RCodeFormatError
		return response.Pack()
	}
	question := query.Questions[0]
	name := strings.ToLower(question.Name.String())
	switch name {
	case fixtureRefused:
		response.Header.RCode = dnsmessage.RCodeRefused
	case fixtureAlias:
		target := dnsmessage.MustNewName(fixtureName)
		response.Answers = append(response.Answers, dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: fixtureTTL},
			Body:   &dnsmessage.CNAMEResource{CNAME: target},
		})
		appendAddressAnswer(&response, target, question.Type)
	case fixtureName:
		appendAddressAnswer(&response, question.Name, question.Type)
	default:
		response.Header.RCode = dnsmessage.RCodeNameError
	}
	return response.Pack()
}

func appendAddressAnswer(response *dnsmessage.Message, name dnsmessage.Name, kind dnsmessage.Type) {
	header := dnsmessage.ResourceHeader{Name: name, Type: kind, Class: dnsmessage.ClassINET, TTL: fixtureTTL}
	switch kind {
	case dnsmessage.TypeA:
		response.Answers = append(response.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AResource{A: fixtureA}})
	case dnsmessage.TypeAAAA:
		response.Answers = append(response.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AAAAResource{AAAA: fixtureAAAA}})
	}
}

func check(ctx context.Context, address string) error {
	for _, network := range []string{"udp", "tcp"} {
		if err := checkNetwork(ctx, network, address); err != nil {
			return fmt.Errorf("%s check: %w", network, err)
		}
	}
	return nil
}

func checkNetwork(ctx context.Context, network, address string) error {
	question := dnsmessage.Question{Name: dnsmessage.MustNewName(fixtureName), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: 42, RecursionDesired: true}, Questions: []dnsmessage.Question{question}}
	wire, err := query.Pack()
	if err != nil {
		return err
	}
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return err
	}
	defer connection.Close()
	deadline, ok := ctx.Deadline()
	if ok {
		_ = connection.SetDeadline(deadline)
	}
	if network == "tcp" {
		var size [2]byte
		binary.BigEndian.PutUint16(size[:], uint16(len(wire)))
		if _, err := connection.Write(append(size[:], wire...)); err != nil {
			return err
		}
		if _, err := io.ReadFull(connection, size[:]); err != nil {
			return err
		}
		wire = make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(connection, wire); err != nil {
			return err
		}
	} else {
		if _, err := connection.Write(wire); err != nil {
			return err
		}
		buffer := make([]byte, 65535)
		n, err := connection.Read(buffer)
		if err != nil {
			return err
		}
		wire = buffer[:n]
	}
	var response dnsmessage.Message
	if err := response.Unpack(wire); err != nil {
		return err
	}
	if response.Header.RCode != dnsmessage.RCodeSuccess || len(response.Answers) != 1 {
		return errors.New("fixture returned an unexpected response")
	}
	answer, ok := response.Answers[0].Body.(*dnsmessage.AResource)
	if !ok || answer.A != fixtureA {
		return errors.New("fixture returned an unexpected address")
	}
	return nil
}
