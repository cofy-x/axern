package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const tcpPoolConnectionsPerWorker = 4

func main() {
	var (
		mode                = flag.String("mode", "verify", "verify, benchmark, benchmark-with-preflight, benchmark-suite-stream, or verify-and-benchmark")
		transport           = flag.String("transport", "", "benchmark transport: tcp-short, tcp-reuse, tcp-pool, udp, or udp-connected")
		tcpAddress          = flag.String("tcp-address", "", "TCP responder address")
		udpAddress          = flag.String("udp-address", "", "UDP responder address")
		icmpAddress         = flag.String("icmp-address", "", "ICMP echo target address")
		expectedSourceIP    = flag.String("expected-source-ip", "", "source IP the external responder must observe")
		timeout             = flag.Duration("timeout", 10*time.Second, "per-probe timeout")
		requests            = flag.Int("requests", 0, "number of benchmark requests to issue")
		concurrency         = flag.Int("concurrency", 16, "number of concurrent benchmark workers")
		warmupRequests      = flag.Int("warmup-requests", 0, "number of warmup requests to issue before benchmarking")
		phaseDelay          = flag.Duration("phase-delay", 250*time.Millisecond, "delay after a benchmark phase begin event before issuing benchmark requests")
		benchmarkTransports = flag.String("benchmark-transports", "", "comma-separated benchmark transports: tcp-short,tcp-reuse,tcp-pool,udp,udp-connected")
	)
	flag.Parse()

	transports, err := selectedBenchmarkTransports(*benchmarkTransports)
	if err != nil {
		fatalf("%v", err)
	}

	switch *mode {
	case "verify":
		if *tcpAddress == "" || *udpAddress == "" || *icmpAddress == "" || *expectedSourceIP == "" {
			fatalf("all of -tcp-address, -udp-address, -icmp-address, and -expected-source-ip are required")
		}
		if err := probeTCP(*tcpAddress, *expectedSourceIP, *timeout); err != nil {
			fatalf("tcp probe failed: %v", err)
		}
		if err := probeUDP(*udpAddress, *expectedSourceIP, *timeout); err != nil {
			fatalf("udp probe failed: %v", err)
		}
		if err := probeICMP(*icmpAddress, *timeout); err != nil {
			fatalf("icmp probe failed: %v", err)
		}
		fmt.Println("egress_probe_ok=true")
	case "verify-and-benchmark":
		if *tcpAddress == "" || *udpAddress == "" || *icmpAddress == "" || *expectedSourceIP == "" {
			fatalf("all of -tcp-address, -udp-address, -icmp-address, and -expected-source-ip are required")
		}
		suite, err := runVerifyAndBenchmark(transports, *tcpAddress, *udpAddress, *icmpAddress, *expectedSourceIP, *requests, *concurrency, *warmupRequests, *timeout)
		if err != nil {
			fatalf("verify-and-benchmark failed: %v", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(suite); err != nil {
			fatalf("encode benchmark suite: %v", err)
		}
	case "benchmark":
		if *expectedSourceIP == "" {
			fatalf("-expected-source-ip is required for benchmark mode")
		}
		summary, err := runBenchmark(*transport, *tcpAddress, *udpAddress, *expectedSourceIP, *requests, *concurrency, *warmupRequests, *timeout)
		if err != nil {
			fatalf("benchmark probe failed: %v", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
			fatalf("encode benchmark summary: %v", err)
		}
	case "benchmark-with-preflight":
		if *tcpAddress == "" || *udpAddress == "" || *icmpAddress == "" || *expectedSourceIP == "" {
			fatalf("all of -tcp-address, -udp-address, -icmp-address, and -expected-source-ip are required")
		}
		summary, err := runBenchmarkWithPreflight(*transport, *tcpAddress, *udpAddress, *icmpAddress, *expectedSourceIP, *requests, *concurrency, *warmupRequests, *timeout)
		if err != nil {
			fatalf("benchmark-with-preflight failed: %v", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
			fatalf("encode benchmark summary: %v", err)
		}
	case "benchmark-suite-stream":
		if *tcpAddress == "" || *udpAddress == "" || *icmpAddress == "" || *expectedSourceIP == "" {
			fatalf("all of -tcp-address, -udp-address, -icmp-address, and -expected-source-ip are required")
		}
		if err := runBenchmarkSuiteStream(transports, *tcpAddress, *udpAddress, *icmpAddress, *expectedSourceIP, *requests, *concurrency, *warmupRequests, *timeout, *phaseDelay); err != nil {
			fatalf("benchmark-suite-stream failed: %v", err)
		}
	default:
		fatalf("unsupported mode %q", *mode)
	}
}

type benchmarkSuite struct {
	TCPShort     natbench.WorkloadSummary `json:"tcpShort"`
	TCPReuse     natbench.WorkloadSummary `json:"tcpReuse"`
	TCPPool      natbench.WorkloadSummary `json:"tcpPool"`
	UDP          natbench.WorkloadSummary `json:"udp"`
	UDPConnected natbench.WorkloadSummary `json:"udpConnected"`
}

func (suite *benchmarkSuite) setSummary(transport string, summary natbench.WorkloadSummary) error {
	switch transport {
	case "tcp-short":
		suite.TCPShort = summary
	case "tcp-reuse":
		suite.TCPReuse = summary
	case "tcp-pool":
		suite.TCPPool = summary
	case "udp":
		suite.UDP = summary
	case "udp-connected":
		suite.UDPConnected = summary
	default:
		return fmt.Errorf("unsupported benchmark transport %q", transport)
	}
	return nil
}

func buildBenchmarkSuite(transports []string, run func(transport string) (natbench.WorkloadSummary, error)) (benchmarkSuite, error) {
	var suite benchmarkSuite
	for _, transport := range transports {
		summary, err := run(transport)
		if err != nil {
			return benchmarkSuite{}, err
		}
		if err := suite.setSummary(transport, summary); err != nil {
			return benchmarkSuite{}, err
		}
	}
	return suite, nil
}

func runVerifyAndBenchmark(transports []string, tcpAddress, udpAddress, icmpAddress, expectedSourceIP string, requests, concurrency, warmupRequests int, timeout time.Duration) (benchmarkSuite, error) {
	if err := probeTCP(tcpAddress, expectedSourceIP, timeout); err != nil {
		return benchmarkSuite{}, fmt.Errorf("tcp preflight failed: %w", err)
	}
	if err := probeUDP(udpAddress, expectedSourceIP, timeout); err != nil {
		return benchmarkSuite{}, fmt.Errorf("udp preflight failed: %w", err)
	}
	if err := probeICMP(icmpAddress, timeout); err != nil {
		return benchmarkSuite{}, fmt.Errorf("icmp preflight failed: %w", err)
	}
	return buildBenchmarkSuite(transports, func(transport string) (natbench.WorkloadSummary, error) {
		summary, err := runBenchmark(transport, tcpAddress, udpAddress, expectedSourceIP, requests, concurrency, warmupRequests, timeout)
		if err != nil {
			return natbench.WorkloadSummary{}, fmt.Errorf("%s benchmark failed: %w", transport, err)
		}
		return summary, nil
	})
}

func runBenchmarkWithPreflight(transport, tcpAddress, udpAddress, icmpAddress, expectedSourceIP string, requests, concurrency, warmupRequests int, timeout time.Duration) (natbench.WorkloadSummary, error) {
	if err := probeTCP(tcpAddress, expectedSourceIP, timeout); err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("tcp preflight failed: %w", err)
	}
	if err := probeUDP(udpAddress, expectedSourceIP, timeout); err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("udp preflight failed: %w", err)
	}
	if err := probeICMP(icmpAddress, timeout); err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("icmp preflight failed: %w", err)
	}
	summary, err := runBenchmark(transport, tcpAddress, udpAddress, expectedSourceIP, requests, concurrency, warmupRequests, timeout)
	if err != nil {
		return natbench.WorkloadSummary{}, err
	}
	return summary, nil
}

func runBenchmarkSuiteStream(transports []string, tcpAddress, udpAddress, icmpAddress, expectedSourceIP string, requests, concurrency, warmupRequests int, timeout, phaseDelay time.Duration) error {
	if err := probeTCP(tcpAddress, expectedSourceIP, timeout); err != nil {
		return fmt.Errorf("tcp preflight failed: %w", err)
	}
	if err := probeUDP(udpAddress, expectedSourceIP, timeout); err != nil {
		return fmt.Errorf("udp preflight failed: %w", err)
	}
	if err := probeICMP(icmpAddress, timeout); err != nil {
		return fmt.Errorf("icmp preflight failed: %w", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	for _, transport := range transports {
		clientBefore := natbench.ReadClientResourceSnapshot()
		if err := encoder.Encode(natbench.BenchmarkStreamEvent{
			Event:     natbench.BenchmarkPhaseBeginEvent,
			Transport: transport,
			Client:    &clientBefore,
		}); err != nil {
			return fmt.Errorf("emit begin event for %s: %w", transport, err)
		}
		if phaseDelay > 0 {
			time.Sleep(phaseDelay)
		}
		sampler := natbench.StartClientResourceSampler(100 * time.Millisecond)
		summary, err := runBenchmark(transport, tcpAddress, udpAddress, expectedSourceIP, requests, concurrency, warmupRequests, timeout)
		clientAfter := natbench.ReadClientResourceSnapshot()
		clientPeak, clientSamples := sampler.Stop(clientAfter)
		if err != nil {
			return fmt.Errorf("%s benchmark failed: %w", transport, err)
		}
		if err := encoder.Encode(natbench.BenchmarkStreamEvent{
			Event:         natbench.BenchmarkPhaseEndEvent,
			Transport:     transport,
			Summary:       &summary,
			Client:        &clientAfter,
			ClientPeak:    &clientPeak,
			ClientSamples: clientSamples,
		}); err != nil {
			return fmt.Errorf("emit end event for %s: %w", transport, err)
		}
	}
	return nil
}

func selectedBenchmarkTransports(raw string) ([]string, error) {
	known := map[string]struct{}{
		"tcp-short":     {},
		"tcp-reuse":     {},
		"tcp-pool":      {},
		"udp":           {},
		"udp-connected": {},
	}
	if strings.TrimSpace(raw) == "" {
		return []string{"tcp-short"}, nil
	}

	transports := make([]string, 0, 3)
	seen := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		transport := strings.TrimSpace(strings.ToLower(part))
		if transport == "" {
			continue
		}
		if _, ok := known[transport]; !ok {
			return nil, fmt.Errorf("unsupported benchmark transport %q", part)
		}
		if _, ok := seen[transport]; ok {
			continue
		}
		seen[transport] = struct{}{}
		transports = append(transports, transport)
	}
	return transports, nil
}

func runBenchmark(transport, tcpAddress, udpAddress, expectedSourceIP string, requests, concurrency, warmupRequests int, timeout time.Duration) (natbench.WorkloadSummary, error) {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "tcp-short":
		if tcpAddress == "" {
			return natbench.WorkloadSummary{}, fmt.Errorf("tcp address is required for tcp short benchmark")
		}
		return natbench.RunWorkloadWarmup(requests, concurrency, warmupRequests, func() error {
			return probeTCP(tcpAddress, expectedSourceIP, timeout)
		}), nil
	case "tcp-reuse":
		if tcpAddress == "" {
			return natbench.WorkloadSummary{}, fmt.Errorf("tcp address is required for tcp reuse benchmark")
		}
		return natbench.RunWorkloadWithWorkerWarmup(requests, concurrency, warmupRequests, func() (func() error, func(), error) {
			probe, err := dialTCPProbe(tcpAddress, expectedSourceIP, timeout)
			if err != nil {
				return nil, nil, err
			}
			return probe.probe, probe.close, nil
		}), nil
	case "tcp-pool":
		if tcpAddress == "" {
			return natbench.WorkloadSummary{}, fmt.Errorf("tcp address is required for tcp pool benchmark")
		}
		return natbench.RunWorkloadWithWorkerWarmup(requests, concurrency, warmupRequests, func() (func() error, func(), error) {
			pool, err := dialTCPProbePool(tcpAddress, expectedSourceIP, timeout, tcpPoolConnectionsPerWorker)
			if err != nil {
				return nil, nil, err
			}
			return pool.probe, pool.close, nil
		}), nil
	case "udp":
		if udpAddress == "" {
			return natbench.WorkloadSummary{}, fmt.Errorf("udp address is required for udp benchmark")
		}
		if err := warmupUDPRoute(tcpAddress, expectedSourceIP, timeout); err != nil {
			return natbench.WorkloadSummary{}, err
		}
		return natbench.RunWorkloadWarmup(requests, concurrency, warmupRequests, func() error {
			return probeUDP(udpAddress, expectedSourceIP, timeout)
		}), nil
	case "udp-connected":
		if udpAddress == "" {
			return natbench.WorkloadSummary{}, fmt.Errorf("udp address is required for udp connected benchmark")
		}
		if err := warmupUDPRoute(tcpAddress, expectedSourceIP, timeout); err != nil {
			return natbench.WorkloadSummary{}, err
		}
		return natbench.RunWorkloadWithWorkerWarmup(requests, concurrency, warmupRequests, func() (func() error, func(), error) {
			conn, err := dialUDP(udpAddress)
			if err != nil {
				return nil, nil, err
			}
			return func() error {
					return probeUDPConn(conn, expectedSourceIP, timeout)
				}, func() {
					_ = conn.Close()
				}, nil
		}), nil
	default:
		return natbench.WorkloadSummary{}, fmt.Errorf("unsupported benchmark transport %q", transport)
	}
}

func warmupUDPRoute(tcpAddress, expectedSourceIP string, timeout time.Duration) error {
	if tcpAddress == "" {
		return nil
	}
	warmupTimeout := timeout * 3
	if warmupTimeout < 30*time.Second {
		warmupTimeout = 30 * time.Second
	}
	if err := waitForReachability(func() error {
		return probeTCP(tcpAddress, expectedSourceIP, timeout)
	}, warmupTimeout); err != nil {
		return fmt.Errorf("warm up udp route via tcp probe: %w", err)
	}
	return nil
}

func waitForReachability(probe func() error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := probe(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return lastErr
}

func probeTCP(address, expectedSourceIP string, timeout time.Duration) error {
	probe, err := dialTCPProbe(address, expectedSourceIP, timeout)
	if err != nil {
		return err
	}
	defer probe.close()
	return nil
}

type tcpProbe struct {
	conn             net.Conn
	reader           *bufio.Reader
	expectedSourceIP string
	timeout          time.Duration
}

func dialTCPProbe(address, expectedSourceIP string, timeout time.Duration) (*tcpProbe, error) {
	conn, err := net.DialTimeout("tcp4", address, timeout)
	if err != nil {
		return nil, err
	}
	probe := &tcpProbe{
		conn:             conn,
		reader:           bufio.NewReader(conn),
		expectedSourceIP: expectedSourceIP,
		timeout:          timeout,
	}
	if err := probe.validateInitialSource(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return probe, nil
}

func (p *tcpProbe) validateInitialSource() error {
	line, err := p.readSourceLine()
	if err != nil {
		return err
	}
	return assertObservedSource(strings.TrimSpace(line), p.expectedSourceIP)
}

func (p *tcpProbe) probe() error {
	if err := p.setDeadline(); err != nil {
		return err
	}
	if _, err := p.conn.Write([]byte("axern-egress-tcp\n")); err != nil {
		return err
	}
	line, err := p.reader.ReadString('\n')
	if err != nil {
		return err
	}
	return assertObservedSource(strings.TrimSpace(line), p.expectedSourceIP)
}

func (p *tcpProbe) readSourceLine() (string, error) {
	if err := p.setDeadline(); err != nil {
		return "", err
	}
	return p.reader.ReadString('\n')
}

func (p *tcpProbe) setDeadline() error {
	timeout := p.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return p.conn.SetDeadline(time.Now().Add(timeout))
}

func (p *tcpProbe) close() {
	if p != nil && p.conn != nil {
		_ = p.conn.Close()
	}
}

type tcpProbePool struct {
	probes []*tcpProbe
	next   int
}

func dialTCPProbePool(address, expectedSourceIP string, timeout time.Duration, size int) (*tcpProbePool, error) {
	if size <= 0 {
		size = 1
	}
	pool := &tcpProbePool{probes: make([]*tcpProbe, 0, size)}
	for i := 0; i < size; i++ {
		probe, err := dialTCPProbe(address, expectedSourceIP, timeout)
		if err != nil {
			pool.close()
			return nil, err
		}
		pool.probes = append(pool.probes, probe)
	}
	return pool, nil
}

func (p *tcpProbePool) probe() error {
	if len(p.probes) == 0 {
		return fmt.Errorf("tcp probe pool is empty")
	}
	probe := p.probes[p.next%len(p.probes)]
	p.next++
	return probe.probe()
}

func (p *tcpProbePool) close() {
	if p == nil {
		return
	}
	for _, probe := range p.probes {
		probe.close()
	}
}

func probeUDP(address, expectedSourceIP string, timeout time.Duration) error {
	conn, err := dialUDP(address)
	if err != nil {
		return err
	}
	defer conn.Close()

	return probeUDPConn(conn, expectedSourceIP, timeout)
}

func dialUDP(address string) (*net.UDPConn, error) {
	remote, err := net.ResolveUDPAddr("udp4", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp4", nil, remote)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func probeUDPConn(conn *net.UDPConn, expectedSourceIP string, timeout time.Duration) error {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte("axern-egress-udp")); err != nil {
		return err
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	return assertObservedSource(strings.TrimSpace(string(buf[:n])), expectedSourceIP)
}

func probeICMP(address string, timeout time.Duration) error {
	targetIP := net.ParseIP(address).To4()
	if targetIP == nil {
		return fmt.Errorf("icmp address %q is not ipv4", address)
	}

	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("open icmp datagram socket: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	echoID := os.Getpid() & 0xffff
	echoSeq := int(time.Now().UnixNano() & 0xffff)
	echoData := []byte("axern-egress-icmp")
	msg := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   echoID,
			Seq:  echoSeq,
			Data: echoData,
		},
	}
	payload, err := msg.Marshal(nil)
	if err != nil {
		return fmt.Errorf("marshal icmp echo request: %w", err)
	}
	if _, err := conn.WriteTo(payload, &net.UDPAddr{IP: targetIP}); err != nil {
		return fmt.Errorf("send icmp echo request: %w", err)
	}

	buf := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			return fmt.Errorf("read icmp echo reply: %w", err)
		}
		if !addrHasIPv4(peer, targetIP) {
			continue
		}

		reply, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), buf[:n])
		if err != nil {
			continue
		}
		if reply.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		echo, ok := reply.Body.(*icmp.Echo)
		if !ok {
			continue
		}
		if len(echo.Data) > 0 && !bytes.Equal(echo.Data, echoData) {
			continue
		}
		return nil
	}
}

func addrHasIPv4(addr net.Addr, want net.IP) bool {
	switch value := addr.(type) {
	case *net.IPAddr:
		return value.IP.To4() != nil && value.IP.To4().Equal(want)
	case *net.UDPAddr:
		return value.IP.To4() != nil && value.IP.To4().Equal(want)
	default:
		return false
	}
}

func assertObservedSource(observed, expectedSourceIP string) error {
	const prefix = "source="
	if !strings.HasPrefix(observed, prefix) {
		return fmt.Errorf("unexpected responder payload %q", observed)
	}
	actual := strings.TrimPrefix(observed, prefix)
	if actual != expectedSourceIP {
		return fmt.Errorf("unexpected observed source ip %q, want %q", actual, expectedSourceIP)
	}
	return nil
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
