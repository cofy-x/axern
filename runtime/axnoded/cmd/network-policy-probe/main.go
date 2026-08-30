package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const allowedFixtureName = "allowed.fixture.axern.test."
const deniedFixtureName = "denied.fixture.axern.test."

type probeResult struct {
	DNSMilliseconds             *float64 `json:"dnsMilliseconds,omitempty"`
	FirstConnectionMilliseconds float64  `json:"firstConnectionMilliseconds"`
	HTTPThroughputMbps          *float64 `json:"httpThroughputMbps,omitempty"`
	TLSThroughputMbps           *float64 `json:"tlsThroughputMbps,omitempty"`
	Operations                  uint64   `json:"operations"`
	Failures                    uint64   `json:"failures"`
	PeakConcurrentSessions      uint32   `json:"peakConcurrentSessions"`
}

type config struct {
	mode          string
	dnsServer     string
	address       string
	host          string
	payloadBytes  int
	concurrency   int
	sustainedTime time.Duration
	timeout       time.Duration
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "network-policy-probe: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("network-policy-probe", flag.ContinueOnError)
	cfg := config{}
	flags.StringVar(&cfg.mode, "policy-mode", "", "unrestricted, dns_deny, strict_domain, or strict_cidr")
	flags.StringVar(&cfg.dnsServer, "dns-server", "", "trusted fixture DNS address")
	flags.StringVar(&cfg.address, "address", "", "fixture IP address")
	flags.StringVar(&cfg.host, "host", strings.TrimSuffix(allowedFixtureName, "."), "HTTP Host and TLS SNI fixture name")
	flags.IntVar(&cfg.payloadBytes, "payload-bytes", 1<<20, "response payload bytes")
	flags.IntVar(&cfg.concurrency, "concurrency", 16, "concurrent sustained sessions")
	flags.DurationVar(&cfg.sustainedTime, "sustained", 10*time.Second, "sustained reliability interval")
	flags.DurationVar(&cfg.timeout, "timeout", 5*time.Second, "per operation timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	result, err := execute(cfg)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func (cfg config) validate() error {
	switch cfg.mode {
	case "unrestricted", "dns_deny", "strict_domain", "strict_cidr":
	default:
		return fmt.Errorf("unsupported policy mode %q", cfg.mode)
	}
	if net.ParseIP(cfg.address) == nil {
		return errors.New("address must be an IP literal")
	}
	if cfg.mode != "strict_cidr" && strings.TrimSpace(cfg.dnsServer) == "" {
		return errors.New("dns-server is required for DNS-capable policy modes")
	}
	if cfg.payloadBytes <= 0 || cfg.concurrency <= 0 || cfg.sustainedTime <= 0 || cfg.timeout <= 0 {
		return errors.New("payload-bytes, concurrency, sustained, and timeout must be positive")
	}
	return nil
}

func execute(cfg config) (probeResult, error) {
	result := probeResult{PeakConcurrentSessions: uint32(cfg.concurrency)}
	if cfg.mode != "strict_cidr" {
		started := time.Now()
		queryType := dnsmessage.TypeA
		if strings.Contains(cfg.address, ":") {
			queryType = dnsmessage.TypeAAAA
		}
		if err := queryDNS(cfg.dnsServer, allowedFixtureName, queryType, dnsmessage.RCodeSuccess, cfg.timeout); err != nil {
			return probeResult{}, err
		}
		if cfg.mode == "dns_deny" {
			if err := queryDNS(cfg.dnsServer, deniedFixtureName, queryType, dnsmessage.RCodeRefused, cfg.timeout); err != nil {
				return probeResult{}, fmt.Errorf("denied DNS probe: %w", err)
			}
		}
		latency := milliseconds(time.Since(started))
		result.DNSMilliseconds = &latency
	}

	firstStarted := time.Now()
	var firstErr error
	if cfg.mode == "strict_cidr" {
		firstErr = rawTCP(net.JoinHostPort(cfg.address, "18080"), cfg.timeout)
	} else {
		firstErr = httpRequest(cfg.address, cfg.host, cfg.payloadBytes, false, cfg.timeout)
	}
	if firstErr != nil {
		return probeResult{}, fmt.Errorf("first connection: %w", firstErr)
	}
	if cfg.mode == "strict_cidr" {
		if err := rawUDP(net.JoinHostPort(cfg.address, "18081"), cfg.timeout); err != nil {
			return probeResult{}, fmt.Errorf("CIDR UDP probe: %w", err)
		}
	}
	result.FirstConnectionMilliseconds = milliseconds(time.Since(firstStarted))

	if cfg.mode != "strict_cidr" {
		httpMbps, err := throughput(cfg, false)
		if err != nil {
			return probeResult{}, err
		}
		tlsMbps, err := throughput(cfg, true)
		if err != nil {
			return probeResult{}, err
		}
		result.HTTPThroughputMbps = &httpMbps
		result.TLSThroughputMbps = &tlsMbps
	}
	operations, failures := sustained(cfg)
	result.Operations = operations
	result.Failures = failures
	if operations == 0 {
		return probeResult{}, errors.New("sustained probe completed no operations")
	}
	return result, nil
}

func queryDNS(server, name string, queryType dnsmessage.Type, expectedRCode dnsmessage.RCode, timeout time.Duration) error {
	question := dnsmessage.Question{Name: dnsmessage.MustNewName(name), Type: queryType, Class: dnsmessage.ClassINET}
	if host, port, err := net.SplitHostPort(server); err != nil || host == "" || port == "" {
		literal := strings.Trim(server, "[]")
		if net.ParseIP(literal) == nil {
			return errors.New("dns-server must be an IP literal or IP:port")
		}
		server = net.JoinHostPort(literal, "53")
	}
	message := dnsmessage.Message{Header: dnsmessage.Header{ID: 1, RecursionDesired: true}, Questions: []dnsmessage.Question{question}}
	wire, err := message.Pack()
	if err != nil {
		return err
	}
	connection, err := net.DialTimeout("udp", server, timeout)
	if err != nil {
		return fmt.Errorf("dial DNS fixture: %w", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))
	if _, err := connection.Write(wire); err != nil {
		return err
	}
	responseWire := make([]byte, 65535)
	n, err := connection.Read(responseWire)
	if err != nil {
		return err
	}
	var response dnsmessage.Message
	if err := response.Unpack(responseWire[:n]); err != nil {
		return err
	}
	if response.Header.RCode != expectedRCode {
		return fmt.Errorf("DNS fixture returned rcode=%s, want %s", response.Header.RCode, expectedRCode)
	}
	if expectedRCode == dnsmessage.RCodeSuccess && len(response.Answers) == 0 {
		return errors.New("DNS fixture returned no address answer")
	}
	return nil
}

func throughput(cfg config, useTLS bool) (float64, error) {
	started := time.Now()
	var bytesRead atomic.Uint64
	var firstErr error
	var errMu sync.Mutex
	var workers sync.WaitGroup
	for worker := 0; worker < cfg.concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := httpRequest(cfg.address, cfg.host, cfg.payloadBytes, useTLS, cfg.timeout); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			bytesRead.Add(uint64(cfg.payloadBytes))
		}()
	}
	workers.Wait()
	if firstErr != nil {
		return 0, firstErr
	}
	seconds := time.Since(started).Seconds()
	return float64(bytesRead.Load()*8) / seconds / 1_000_000, nil
}

func sustained(cfg config) (uint64, uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.sustainedTime)
	defer cancel()
	var operations atomic.Uint64
	var failures atomic.Uint64
	var workers sync.WaitGroup
	for worker := 0; worker < cfg.concurrency; worker++ {
		worker := worker
		workers.Add(1)
		go func() {
			defer workers.Done()
			for ctx.Err() == nil {
				var err error
				if cfg.mode == "strict_cidr" {
					if worker%2 == 0 {
						err = rawTCP(net.JoinHostPort(cfg.address, "18080"), cfg.timeout)
					} else {
						err = rawUDP(net.JoinHostPort(cfg.address, "18081"), cfg.timeout)
					}
				} else {
					err = httpRequest(cfg.address, cfg.host, 1, false, cfg.timeout)
				}
				operations.Add(1)
				if err != nil {
					failures.Add(1)
				}
			}
		}()
	}
	workers.Wait()
	return operations.Load(), failures.Load()
}

func rawTCP(address string, timeout time.Duration) error {
	connection, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(connection, "probe\n"); err != nil {
		return err
	}
	_, err = bufio.NewReader(connection).ReadString('\n')
	return err
}

func rawUDP(address string, timeout time.Duration) error {
	connection, err := net.DialTimeout("udp", address, timeout)
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))
	payload := []byte("probe")
	if _, err := connection.Write(payload); err != nil {
		return err
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, response); err != nil {
		return err
	}
	if string(response) != string(payload) {
		return errors.New("UDP fixture returned unexpected payload")
	}
	return nil
}

func httpRequest(address, host string, payloadBytes int, useTLS bool, timeout time.Duration) error {
	port := "80"
	if useTLS {
		port = "443"
	}
	dialer := &net.Dialer{Timeout: timeout}
	connection, err := dialer.Dial("tcp", net.JoinHostPort(address, port))
	if err != nil {
		return err
	}
	_ = connection.SetDeadline(time.Now().Add(timeout))
	if useTLS {
		tlsConnection := tls.Client(connection, &tls.Config{ServerName: host, InsecureSkipVerify: true}) // fixture certificate is intentionally ephemeral.
		handshakeCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := tlsConnection.HandshakeContext(handshakeCtx); err != nil {
			_ = connection.Close()
			return err
		}
		connection = tlsConnection
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(connection, "GET /payload?bytes=%d HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", payloadBytes, host); err != nil {
		return err
	}
	reader := bufio.NewReader(connection)
	status, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(status, " 200 ") {
		return fmt.Errorf("HTTP fixture status %q: %w", strings.TrimSpace(status), err)
	}
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if key, value, found := strings.Cut(line, ":"); found && strings.EqualFold(key, "Content-Length") {
			if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &contentLength); err != nil {
				return err
			}
		}
	}
	if contentLength != payloadBytes {
		return fmt.Errorf("HTTP fixture content length = %d, want %d", contentLength, payloadBytes)
	}
	_, err = io.CopyN(io.Discard, reader, int64(contentLength))
	return err
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
