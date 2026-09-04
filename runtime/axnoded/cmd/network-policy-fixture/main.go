package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	allowedName    = "allowed.fixture.axern.test."
	deniedName     = "denied.fixture.axern.test."
	fixtureTTL     = 30
	maxPayloadSize = 16 << 20
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "network-policy-fixture: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("network-policy-fixture", flag.ContinueOnError)
	listenIP := flags.String("listen-ip", "", "fixture listener IP")
	answerIP := flags.String("answer-ip", "", "DNS answer IP")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	listen := net.ParseIP(strings.TrimSpace(*listenIP))
	answer := net.ParseIP(strings.TrimSpace(*answerIP))
	if listen == nil || answer == nil || (listen.To4() == nil) != (answer.To4() == nil) {
		return errors.New("listen-ip and answer-ip must be valid literals from the same address family")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fixture, err := startFixture(ctx, listen.String(), answer)
	if err != nil {
		return err
	}
	defer fixture.close()
	if _, err := fmt.Fprintln(output, "network_policy_fixture_ready=true"); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

type fixture struct {
	closers []io.Closer
}

func (fixture *fixture) close() {
	for _, closer := range fixture.closers {
		_ = closer.Close()
	}
}

func startFixture(ctx context.Context, listenIP string, answerIP net.IP) (*fixture, error) {
	servers := &fixture{}
	closeOnError := func(err error) (*fixture, error) {
		servers.close()
		return nil, err
	}
	dnsTCP, err := net.Listen("tcp", net.JoinHostPort(listenIP, "53"))
	if err != nil {
		return nil, fmt.Errorf("listen DNS TCP: %w", err)
	}
	servers.closers = append(servers.closers, dnsTCP)
	dnsUDP, err := net.ListenPacket("udp", net.JoinHostPort(listenIP, "53"))
	if err != nil {
		return closeOnError(fmt.Errorf("listen DNS UDP: %w", err))
	}
	servers.closers = append(servers.closers, dnsUDP)

	handler := http.HandlerFunc(servePayload)
	httpListener, err := net.Listen("tcp", net.JoinHostPort(listenIP, "80"))
	if err != nil {
		return closeOnError(fmt.Errorf("listen HTTP: %w", err))
	}
	servers.closers = append(servers.closers, httpListener)
	certificate, err := fixtureCertificate()
	if err != nil {
		return closeOnError(err)
	}
	tlsBase, err := net.Listen("tcp", net.JoinHostPort(listenIP, "443"))
	if err != nil {
		return closeOnError(fmt.Errorf("listen TLS: %w", err))
	}
	tlsListener := tls.NewListener(tlsBase, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	servers.closers = append(servers.closers, tlsListener)

	rawTCP, err := net.Listen("tcp", net.JoinHostPort(listenIP, "18080"))
	if err != nil {
		return closeOnError(fmt.Errorf("listen raw TCP: %w", err))
	}
	servers.closers = append(servers.closers, rawTCP)
	rawUDP, err := net.ListenPacket("udp", net.JoinHostPort(listenIP, "18081"))
	if err != nil {
		return closeOnError(fmt.Errorf("listen raw UDP: %w", err))
	}
	servers.closers = append(servers.closers, rawUDP)

	go serveDNSUDP(ctx, dnsUDP, answerIP)
	go serveDNSTCP(ctx, dnsTCP, answerIP)
	go (&http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second}).Serve(httpListener)
	go (&http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second}).Serve(tlsListener)
	go serveRawTCP(ctx, rawTCP)
	go serveRawUDP(ctx, rawUDP)
	return servers, nil
}

func servePayload(response http.ResponseWriter, request *http.Request) {
	if request.Host != strings.TrimSuffix(allowedName, ".") {
		http.Error(response, "unexpected fixture host", http.StatusMisdirectedRequest)
		return
	}
	bytes := 1
	if raw := request.URL.Query().Get("bytes"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > maxPayloadSize {
			http.Error(response, "invalid payload size", http.StatusBadRequest)
			return
		}
		bytes = parsed
	}
	response.Header().Set("Content-Length", strconv.Itoa(bytes))
	response.WriteHeader(http.StatusOK)
	_, _ = io.CopyN(response, zeroReader{}, int64(bytes))
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

func serveDNSUDP(ctx context.Context, connection net.PacketConn, answer net.IP) {
	buffer := make([]byte, 65535)
	for ctx.Err() == nil {
		_ = connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, peer, err := connection.ReadFrom(buffer)
		if err != nil {
			continue
		}
		if response, err := dnsResponse(buffer[:n], answer); err == nil {
			_, _ = connection.WriteTo(response, peer)
		}
	}
}

func serveDNSTCP(ctx context.Context, listener net.Listener, answer net.IP) {
	for ctx.Err() == nil {
		if tcp, ok := listener.(*net.TCPListener); ok {
			_ = tcp.SetDeadline(time.Now().Add(250 * time.Millisecond))
		}
		connection, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleDNSTCP(connection, answer)
	}
}

func handleDNSTCP(connection net.Conn, answer net.IP) {
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
	response, err := dnsResponse(request, answer)
	if err != nil || len(response) > 65535 {
		return
	}
	binary.BigEndian.PutUint16(size[:], uint16(len(response)))
	_, _ = connection.Write(append(size[:], response...))
}

func dnsResponse(wire []byte, answer net.IP) ([]byte, error) {
	var query dnsmessage.Message
	if err := query.Unpack(wire); err != nil {
		return nil, err
	}
	response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true, Authoritative: true, RecursionDesired: query.Header.RecursionDesired, RecursionAvailable: true}, Questions: append([]dnsmessage.Question(nil), query.Questions...)}
	if len(query.Questions) != 1 || query.Questions[0].Class != dnsmessage.ClassINET {
		response.Header.RCode = dnsmessage.RCodeFormatError
		return response.Pack()
	}
	question := query.Questions[0]
	switch strings.ToLower(question.Name.String()) {
	case deniedName, allowedName:
		header := dnsmessage.ResourceHeader{Name: question.Name, Type: question.Type, Class: dnsmessage.ClassINET, TTL: fixtureTTL}
		if ipv4 := answer.To4(); question.Type == dnsmessage.TypeA && ipv4 != nil {
			var value [4]byte
			copy(value[:], ipv4)
			response.Answers = append(response.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AResource{A: value}})
		} else if question.Type == dnsmessage.TypeAAAA && answer.To4() == nil {
			var value [16]byte
			copy(value[:], answer.To16())
			response.Answers = append(response.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AAAAResource{AAAA: value}})
		}
	default:
		response.Header.RCode = dnsmessage.RCodeNameError
	}
	return response.Pack()
}

func serveRawTCP(ctx context.Context, listener net.Listener) {
	for ctx.Err() == nil {
		if tcp, ok := listener.(*net.TCPListener); ok {
			_ = tcp.SetDeadline(time.Now().Add(250 * time.Millisecond))
		}
		connection, err := listener.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer connection.Close()
			reader := bufio.NewReader(connection)
			for ctx.Err() == nil {
				_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
				if _, err := reader.ReadString('\n'); err != nil {
					return
				}
				if _, err := io.WriteString(connection, "ok\n"); err != nil {
					return
				}
			}
		}()
	}
}

func serveRawUDP(ctx context.Context, connection net.PacketConn) {
	buffer := make([]byte, 65535)
	for ctx.Err() == nil {
		_ = connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, peer, err := connection.ReadFrom(buffer)
		if err == nil {
			_, _ = connection.WriteTo(buffer[:n], peer)
		}
	}
}

func fixtureCertificate() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: strings.TrimSuffix(allowedName, ".")}, DNSNames: []string{strings.TrimSuffix(allowedName, ".")}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return tls.X509KeyPair(certificatePEM, keyPEM)
}
