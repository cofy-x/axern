package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/internal/egress"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

const (
	allowedFixtureDomain = "allowed.fixture.axern.test"
	deniedFixtureDomain  = "denied.fixture.axern.test"
)

type config struct {
	runtimeName       string
	networkBackend    string
	ipFamily          string
	policyMode        string
	samples           int
	concurrency       int
	payloadBytes      int
	sustainedSeconds  int
	ruleScaleCounts   []uint32
	output            string
	axnodedSocket     string
	egressdSocket     string
	egressdBinary     string
	rootfs            string
	helperDir         string
	fixtureAddress    string
	dnsServer         string
	operationTimeout  time.Duration
	startupTimeout    time.Duration
	recoveryNamespace string
}

type distribution struct {
	Samples int     `json:"samples"`
	P50     float64 `json:"p50"`
	P95     float64 `json:"p95"`
	P99     float64 `json:"p99"`
	Max     float64 `json:"max"`
}

type ruleScalePoint struct {
	Rules          uint32  `json:"rules"`
	PrepareP95MS   float64 `json:"prepareP95Milliseconds"`
	ReconcileP95MS float64 `json:"reconcileP95Milliseconds"`
	RSSBytes       uint64  `json:"rssBytes"`
}

type scenarioMetrics struct {
	PrepareLatencyMS            *distribution    `json:"prepareLatencyMilliseconds,omitempty"`
	SandboxStartLatencyMS       *distribution    `json:"sandboxStartLatencyMilliseconds,omitempty"`
	DNSLatencyMS                *distribution    `json:"dnsLatencyMilliseconds,omitempty"`
	FirstConnectionLatencyMS    *distribution    `json:"firstConnectionLatencyMilliseconds,omitempty"`
	RestartConvergenceLatencyMS *distribution    `json:"restartConvergenceLatencyMilliseconds,omitempty"`
	HTTPThroughputMbps          *float64         `json:"httpThroughputMbps,omitempty"`
	TLSThroughputMbps           *float64         `json:"tlsThroughputMbps,omitempty"`
	MaxRSSBytes                 uint64           `json:"maxRssBytes"`
	PeakConcurrentSessions      uint32           `json:"peakConcurrentSessions"`
	Operations                  uint64           `json:"operations"`
	Failures                    uint64           `json:"failures"`
	RuleScale                   []ruleScalePoint `json:"ruleScale,omitempty"`
}

type scenarioResult struct {
	Runtime        string          `json:"runtime"`
	NetworkBackend string          `json:"networkBackend"`
	IPFamily       string          `json:"ipFamily"`
	PolicyMode     string          `json:"policyMode"`
	Metrics        scenarioMetrics `json:"metrics"`
}

type probeResult struct {
	DNSMilliseconds             *float64 `json:"dnsMilliseconds,omitempty"`
	FirstConnectionMilliseconds float64  `json:"firstConnectionMilliseconds"`
	HTTPThroughputMbps          *float64 `json:"httpThroughputMbps,omitempty"`
	TLSThroughputMbps           *float64 `json:"tlsThroughputMbps,omitempty"`
	Operations                  uint64   `json:"operations"`
	Failures                    uint64   `json:"failures"`
	PeakConcurrentSessions      uint32   `json:"peakConcurrentSessions"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "verify-network-policy-qualification: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	result, err := qualify(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.output), 0o755); err != nil {
		return err
	}
	file, err := os.Create(cfg.output)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(result)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func parseFlags(args []string) (config, error) {
	flags := flag.NewFlagSet("verify-network-policy-qualification", flag.ContinueOnError)
	cfg := config{}
	ruleCounts := ""
	flags.StringVar(&cfg.runtimeName, "runtime", "", "runc or runsc")
	flags.StringVar(&cfg.networkBackend, "network-backend", "", "bridge or ebpf")
	flags.StringVar(&cfg.ipFamily, "ip-family", "", "ipv4 or ipv6")
	flags.StringVar(&cfg.policyMode, "policy-mode", "", "unrestricted, dns_deny, strict_domain, or strict_cidr")
	flags.IntVar(&cfg.samples, "samples", 0, "latency samples")
	flags.IntVar(&cfg.concurrency, "concurrency", 0, "concurrent sessions")
	flags.IntVar(&cfg.payloadBytes, "payload-bytes", 0, "relay payload bytes")
	flags.IntVar(&cfg.sustainedSeconds, "sustained-seconds", 0, "total sustained reliability interval")
	flags.StringVar(&ruleCounts, "rule-scale-counts", "", "comma-separated policy rule counts")
	flags.StringVar(&cfg.output, "output", "", "scenario JSON output")
	flags.StringVar(&cfg.axnodedSocket, "axnoded-socket", "/run/axnoded/axnoded.sock", "axnoded Unix socket")
	flags.StringVar(&cfg.egressdSocket, "egressd-socket", "/run/egressd/egressd.sock", "egressd Unix socket")
	flags.StringVar(&cfg.egressdBinary, "egressd-binary", "/usr/local/bin/egressd", "egressd binary used for recovery qualification")
	flags.StringVar(&cfg.rootfs, "rootfs", "/opt/sample-rootfs", "read-only local rootfs fixture")
	flags.StringVar(&cfg.helperDir, "helper-dir", "/usr/local/libexec/axnoded", "sandbox helper binary directory")
	flags.StringVar(&cfg.fixtureAddress, "fixture-address", os.Getenv("NETWORK_POLICY_QUALIFICATION_FIXTURE_ADDRESS"), "isolated fixture IP")
	flags.StringVar(&cfg.dnsServer, "dns-server", os.Getenv("NETWORK_POLICY_QUALIFICATION_DNS_SERVER"), "isolated DNS fixture IP")
	flags.DurationVar(&cfg.operationTimeout, "operation-timeout", 10*time.Second, "per probe and egress operation timeout")
	flags.DurationVar(&cfg.startupTimeout, "startup-timeout", 2*time.Minute, "sandbox startup timeout")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, errors.New("unexpected positional arguments")
	}
	counts, err := parseRuleCounts(ruleCounts)
	if err != nil {
		return config{}, err
	}
	cfg.ruleScaleCounts = counts
	if err := cfg.validate(); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func (cfg *config) validate() error {
	if cfg.runtimeName != "runc" && cfg.runtimeName != "runsc" {
		return fmt.Errorf("unsupported runtime %q", cfg.runtimeName)
	}
	if cfg.networkBackend != "bridge" && cfg.networkBackend != "ebpf" {
		return fmt.Errorf("unsupported network backend %q", cfg.networkBackend)
	}
	if cfg.ipFamily != "ipv4" && cfg.ipFamily != "ipv6" {
		return fmt.Errorf("unsupported IP family %q", cfg.ipFamily)
	}
	switch cfg.policyMode {
	case "unrestricted", "dns_deny", "strict_domain", "strict_cidr":
	default:
		return fmt.Errorf("unsupported policy mode %q", cfg.policyMode)
	}
	fixtureIP := net.ParseIP(strings.TrimSpace(cfg.fixtureAddress))
	dnsIP := net.ParseIP(strings.Trim(strings.TrimSpace(cfg.dnsServer), "[]"))
	if fixtureIP == nil || dnsIP == nil {
		return errors.New("fixture-address and dns-server must be IP literals")
	}
	wantIPv4 := cfg.ipFamily == "ipv4"
	if (fixtureIP.To4() != nil) != wantIPv4 || (dnsIP.To4() != nil) != wantIPv4 {
		return errors.New("fixture addresses do not match ip-family")
	}
	if cfg.samples <= 0 || cfg.concurrency <= 0 || cfg.payloadBytes <= 0 || cfg.sustainedSeconds <= 0 || len(cfg.ruleScaleCounts) == 0 {
		return errors.New("samples, concurrency, payload, sustained interval, and rule scale counts must be positive")
	}
	if cfg.output == "" || cfg.operationTimeout <= 0 || cfg.startupTimeout <= 0 {
		return errors.New("output and positive operation/startup timeouts are required")
	}
	if cfg.recoveryNamespace == "" {
		cfg.recoveryNamespace = fmt.Sprintf("axern-qual-%d", os.Getpid())
	}
	return nil
}

func qualify(cfg config) (scenarioResult, error) {
	clients, err := verifyutil.DialNodeClients(cfg.axnodedSocket)
	if err != nil {
		return scenarioResult{}, err
	}
	defer clients.Close()
	egressClient, err := egress.Dial(context.Background(), cfg.egressdSocket)
	if err != nil {
		return scenarioResult{}, err
	}
	defer egressClient.Close()

	policy := policyFor(cfg.policyMode, cfg.fixtureAddress)
	prepareValues, err := measurePrepare(cfg, egressClient, policy)
	if err != nil {
		return scenarioResult{}, err
	}
	startValues := make([]float64, 0, cfg.samples)
	dnsValues := make([]float64, 0, cfg.samples)
	firstValues := make([]float64, 0, cfg.samples)
	var httpValues, tlsValues []float64
	var operations, failures uint64
	var peakSessions uint32
	segment := time.Duration(cfg.sustainedSeconds) * time.Second / time.Duration(cfg.samples)
	if segment < 10*time.Millisecond {
		segment = 10 * time.Millisecond
	}
	for sample := 0; sample < cfg.samples; sample++ {
		probe, startMS, err := runSandboxSample(cfg, clients, policy, sample, segment)
		if err != nil {
			return scenarioResult{}, err
		}
		startValues = append(startValues, startMS)
		firstValues = append(firstValues, probe.FirstConnectionMilliseconds)
		if probe.DNSMilliseconds != nil {
			dnsValues = append(dnsValues, *probe.DNSMilliseconds)
		}
		if probe.HTTPThroughputMbps != nil {
			httpValues = append(httpValues, *probe.HTTPThroughputMbps)
		}
		if probe.TLSThroughputMbps != nil {
			tlsValues = append(tlsValues, *probe.TLSThroughputMbps)
		}
		operations += probe.Operations
		failures += probe.Failures
		if probe.PeakConcurrentSessions > peakSessions {
			peakSessions = probe.PeakConcurrentSessions
		}
	}
	restartValues, err := measureRestartConvergence(cfg)
	if err != nil {
		return scenarioResult{}, err
	}
	scale, maxRSS, err := measureRuleScale(cfg, egressClient)
	if err != nil {
		return scenarioResult{}, err
	}
	metrics := scenarioMetrics{
		PrepareLatencyMS:            makeDistribution(prepareValues),
		SandboxStartLatencyMS:       makeDistribution(startValues),
		FirstConnectionLatencyMS:    makeDistribution(firstValues),
		RestartConvergenceLatencyMS: makeDistribution(restartValues),
		HTTPThroughputMbps:          meanPointer(httpValues),
		TLSThroughputMbps:           meanPointer(tlsValues),
		MaxRSSBytes:                 maxRSS,
		PeakConcurrentSessions:      peakSessions,
		Operations:                  operations,
		Failures:                    failures,
		RuleScale:                   scale,
	}
	if len(dnsValues) > 0 {
		metrics.DNSLatencyMS = makeDistribution(dnsValues)
	}
	return scenarioResult{Runtime: cfg.runtimeName, NetworkBackend: cfg.networkBackend, IPFamily: cfg.ipFamily, PolicyMode: cfg.policyMode, Metrics: metrics}, nil
}

func runSandboxSample(cfg config, clients *verifyutil.NodeClients, policy *commonv1.NetworkEgressPolicy, sample int, sustained time.Duration) (probeResult, float64, error) {
	id := verifyutil.NewSandboxID(fmt.Sprintf("netpol-qual-%s-%s-%s-%d", cfg.runtimeName, cfg.ipFamily, cfg.policyMode, sample))
	stdoutPath := filepath.Join("/tmp", id+".stdout")
	stderrPath := filepath.Join("/tmp", id+".stderr")
	defer os.Remove(stdoutPath)
	defer os.Remove(stderrPath)
	arguments := []string{
		"/axnoded-bin/network-policy-probe",
		"-policy-mode", cfg.policyMode,
		"-dns-server", cfg.dnsServer,
		"-address", cfg.fixtureAddress,
		"-payload-bytes", strconv.Itoa(cfg.payloadBytes),
		"-concurrency", strconv.Itoa(cfg.concurrency),
		"-sustained", sustained.String(),
		"-timeout", cfg.operationTimeout.String(),
	}
	spec := &privatenodev1.ResolvedExecutionConfig{
		RuntimeClass: cfg.runtimeName, Cwd: "/", LocalRootfsPath: cfg.rootfs, Argv: arguments,
		StdoutPath: stdoutPath, StderrPath: stderrPath,
		Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT, EgressPolicy: policy},
		Mounts: []*privatenodev1.SandboxMount{
			{Type: "bind", Source: cfg.helperDir, Target: "/axnoded-bin", Options: []string{"rbind", "ro"}},
			{Type: "tmpfs", Source: "tmpfs", Target: "/tmp", Options: []string{"nosuid", "nodev", "mode=1777"}},
		},
	}
	startCtx, cancelStart := context.WithTimeout(context.Background(), cfg.startupTimeout)
	started := time.Now()
	handle, err := verifyutil.CreateAllocation(startCtx, clients, id, spec)
	startMS := milliseconds(time.Since(started))
	cancelStart()
	if err != nil {
		return probeResult{}, 0, fmt.Errorf("create sample %d: %w", sample, err)
	}
	defer func() {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = handle.Delete(deleteCtx, 0)
	}()
	waitCtx, cancelWait := context.WithTimeout(context.Background(), cfg.startupTimeout+time.Duration(cfg.sustainedSeconds)*time.Second)
	waitResponse, err := handle.Wait(waitCtx)
	cancelWait()
	stdout, stdoutErr := os.ReadFile(stdoutPath)
	stderr, _ := os.ReadFile(stderrPath)
	if err != nil {
		dumpActivePolicyDiagnostics()
		return probeResult{}, 0, fmt.Errorf("wait sample %d: %w", sample, err)
	}
	if waitResponse.GetExitCode() != 0 {
		dumpActivePolicyDiagnostics()
		return probeResult{}, 0, fmt.Errorf("sample %d exit=%d stderr=%q", sample, waitResponse.GetExitCode(), strings.TrimSpace(string(stderr)))
	}
	if stdoutErr != nil {
		return probeResult{}, 0, stdoutErr
	}
	var probe probeResult
	decoder := json.NewDecoder(strings.NewReader(string(stdout)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&probe); err != nil {
		return probeResult{}, 0, fmt.Errorf("decode sample %d output: %w", sample, err)
	}
	return probe, startMS, nil
}

func dumpActivePolicyDiagnostics() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "nft", "-a", "list", "table", "inet", "axern_egress").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "qualification nft diagnostics unavailable: %v\n", err)
		return
	}
	const maxDiagnosticBytes = 64 << 10
	if len(output) > maxDiagnosticBytes {
		output = output[len(output)-maxDiagnosticBytes:]
	}
	fmt.Fprintf(os.Stderr, "qualification active nft diagnostics:\n%s\n", output)
}

func policyFor(mode, address string) *commonv1.NetworkEgressPolicy {
	switch mode {
	case "dns_deny":
		return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{deniedFixtureDomain}}}}
	case "strict_domain":
		return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedDomains: []string{allowedFixtureDomain}}}}
	case "strict_cidr":
		bits := 32
		if strings.Contains(address, ":") {
			bits = 128
		}
		prefix := fmt.Sprintf("%s/%d", address, bits)
		return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedCidrs: []*commonv1.CIDREgressRule{
			{Cidr: prefix, Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 18080, End: 18080}}},
			{Cidr: prefix, Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_UDP, Ports: []*commonv1.PortRange{{Start: 18081, End: 18081}}},
		}}}}
	default:
		return nil
	}
}

func measurePrepare(cfg config, client *egress.Client, policy *commonv1.NetworkEgressPolicy) ([]float64, error) {
	values := make([]float64, cfg.samples)
	if policy == nil {
		return values, nil
	}
	upstreams := []string(nil)
	if cfg.policyMode == "dns_deny" || cfg.policyMode == "strict_domain" {
		upstreams = []string{cfg.dnsServer}
	}
	for sample := 0; sample < cfg.samples; sample++ {
		allocationID := fmt.Sprintf("qualification-prepare-%d-%d", os.Getpid(), sample)
		ctx, cancel := context.WithTimeout(context.Background(), cfg.operationTimeout)
		started := time.Now()
		_, err := client.Prepare(ctx, allocationID, 1, qualificationSourceIP(cfg.ipFamily), policy, 1, upstreams)
		values[sample] = milliseconds(time.Since(started))
		cancel()
		if err != nil {
			return nil, fmt.Errorf("measure policy prepare: %w", err)
		}
		deleteCtx, cancelDelete := context.WithTimeout(context.Background(), cfg.operationTimeout)
		err = client.Delete(deleteCtx, allocationID, 1)
		cancelDelete()
		if err != nil {
			return nil, fmt.Errorf("delete measured policy: %w", err)
		}
	}
	return values, nil
}

func measureRuleScale(cfg config, client *egress.Client) ([]ruleScalePoint, uint64, error) {
	points := make([]ruleScalePoint, 0, len(cfg.ruleScaleCounts))
	maxRSS, _ := egressdRSSBytes()
	for _, count := range cfg.ruleScaleCounts {
		prepareValues := make([]float64, 0, cfg.samples)
		reconcileValues := make([]float64, 0, cfg.samples)
		for sample := 0; sample < cfg.samples; sample++ {
			allocationID := fmt.Sprintf("qualification-scale-%d-%d-%d", os.Getpid(), count, sample)
			policy := scalePolicy(count, cfg.ipFamily)
			ctx, cancel := context.WithTimeout(context.Background(), cfg.operationTimeout)
			started := time.Now()
			prepared, err := client.Prepare(ctx, allocationID, 1, qualificationSourceIP(cfg.ipFamily), policy, 1, nil)
			prepareValues = append(prepareValues, milliseconds(time.Since(started)))
			cancel()
			if err != nil {
				return nil, 0, fmt.Errorf("prepare %d-rule policy: %w", count, err)
			}
			active := []*runtimeegressv1.ActiveEgressPolicy{{AllocationID: allocationID, Attempt: 1, SandboxIp: prepared.GetSandboxIp(), PolicyDigest: prepared.GetPolicyDigest(), ExecutionRevision: prepared.GetExecutionRevision()}}
			reconcileCtx, cancelReconcile := context.WithTimeout(context.Background(), cfg.operationTimeout)
			started = time.Now()
			_, err = client.Reconcile(reconcileCtx, active)
			reconcileValues = append(reconcileValues, milliseconds(time.Since(started)))
			cancelReconcile()
			if err != nil {
				return nil, 0, fmt.Errorf("reconcile %d-rule policy: %w", count, err)
			}
			if rss, rssErr := egressdRSSBytes(); rssErr == nil && rss > maxRSS {
				maxRSS = rss
			}
			deleteCtx, cancelDelete := context.WithTimeout(context.Background(), cfg.operationTimeout)
			err = client.Delete(deleteCtx, allocationID, 1)
			cancelDelete()
			if err != nil {
				return nil, 0, err
			}
		}
		points = append(points, ruleScalePoint{Rules: count, PrepareP95MS: percentile(prepareValues, 0.95), ReconcileP95MS: percentile(reconcileValues, 0.95), RSSBytes: maxRSS})
	}
	if maxRSS == 0 {
		return nil, 0, errors.New("could not measure egressd RSS")
	}
	return points, maxRSS, nil
}

func scalePolicy(count uint32, family string) *commonv1.NetworkEgressPolicy {
	rules := make([]*commonv1.CIDREgressRule, 0, count)
	for index := uint32(0); index < count; index++ {
		cidr := fmt.Sprintf("2001:db8:100:%x::1/128", index)
		if family == "ipv4" {
			third := (index / 254) % 2
			fourth := index%254 + 1
			cidr = fmt.Sprintf("198.%d.%d.%d/32", 18+third, (index/508)%254, fourth)
		}
		rules = append(rules, &commonv1.CIDREgressRule{Cidr: cidr, Protocol: commonv1.EgressProtocol_EGRESS_PROTOCOL_TCP, Ports: []*commonv1.PortRange{{Start: 18080, End: 18080}}})
	}
	return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedCidrs: rules}}}
}

func measureRestartConvergence(cfg config) ([]float64, error) {
	values := make([]float64, 0, cfg.samples)
	for sample := 0; sample < cfg.samples; sample++ {
		value, err := oneRestartConvergence(cfg, sample)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func oneRestartConvergence(cfg config, sample int) (float64, error) {
	namespace := fmt.Sprintf("%s-%d", cfg.recoveryNamespace, sample)
	root, err := os.MkdirTemp("", "axern-egress-recovery-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(root)
	_ = exec.Command("ip", "netns", "del", namespace).Run()
	if output, err := exec.Command("ip", "netns", "add", namespace).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("create recovery namespace: %w: %s", err, strings.TrimSpace(string(output)))
	}
	defer exec.Command("ip", "netns", "del", namespace).Run()
	if output, err := exec.Command("ip", "netns", "exec", namespace, "ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		return 0, fmt.Errorf("activate recovery namespace loopback: %w: %s", err, strings.TrimSpace(string(output)))
	}
	socket := filepath.Join(root, "egressd.sock")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		return 0, err
	}
	start := func() (*exec.Cmd, error) {
		command := exec.Command("ip", "netns", "exec", namespace, cfg.egressdBinary, "-root", state, "-socket", socket)
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if err := command.Start(); err != nil {
			return nil, err
		}
		return command, nil
	}
	command, err := start()
	if err != nil {
		return 0, err
	}
	stop := func() {
		if command != nil && command.Process != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}
	defer stop()
	client, err := waitEgressClient(socket, cfg.operationTimeout)
	if err != nil {
		return 0, err
	}
	policy := scalePolicy(1, cfg.ipFamily)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.operationTimeout)
	prepared, err := client.Prepare(ctx, "recovery", 1, qualificationSourceIP(cfg.ipFamily), policy, 1, nil)
	cancel()
	_ = client.Close()
	if err != nil {
		return 0, err
	}
	stop()
	command = nil
	_ = os.Remove(socket)
	started := time.Now()
	command, err = start()
	if err != nil {
		return 0, err
	}
	client, err = waitEgressClient(socket, cfg.operationTimeout)
	if err != nil {
		return 0, err
	}
	defer client.Close()
	ctx, cancel = context.WithTimeout(context.Background(), cfg.operationTimeout)
	recovered, err := client.Get(ctx, prepared.GetAllocationID(), prepared.GetAttempt())
	cancel()
	if err != nil || recovered.GetRecoveryState() != runtimeegressv1.EgressPolicyRecoveryState_EGRESS_POLICY_RECOVERY_STATE_RECOVERED {
		return 0, fmt.Errorf("recovered policy proof unavailable: state=%s err=%v", recovered.GetRecoveryState(), err)
	}
	return milliseconds(time.Since(started)), nil
}

func waitEgressClient(socket string, timeout time.Duration) (*egress.Client, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := egress.Dial(context.Background(), socket)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			health, healthErr := client.Health(ctx)
			cancel()
			if healthErr == nil && health.GetStatus() == runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_OK {
				return client, nil
			}
			_ = client.Close()
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf("egressd did not become healthy at %s", socket)
}

func egressdRSSBytes() (uint64, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	var maximum uint64
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "egressd" {
			continue
		}
		status, err := os.Open(filepath.Join("/proc", entry.Name(), "status"))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(status)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) == 3 && fields[0] == "VmRSS:" && fields[2] == "kB" {
				kilobytes, _ := strconv.ParseUint(fields[1], 10, 64)
				if kilobytes*1024 > maximum {
					maximum = kilobytes * 1024
				}
			}
		}
		_ = status.Close()
	}
	if maximum == 0 {
		return 0, errors.New("egressd process RSS is unavailable")
	}
	return maximum, nil
}

func qualificationSourceIP(family string) string {
	if family == "ipv6" {
		return "2001:db8:ffff::254"
	}
	return "198.18.255.254"
}

func parseRuleCounts(raw string) ([]uint32, error) {
	seen := map[uint32]struct{}{}
	var values []uint32
	for _, part := range strings.Split(raw, ",") {
		parsed, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32)
		if err != nil || parsed == 0 || parsed > 256 {
			return nil, fmt.Errorf("invalid rule scale count %q", part)
		}
		value := uint32(parsed)
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	if len(values) == 0 {
		return nil, errors.New("rule-scale-counts is required")
	}
	return values, nil
}

func makeDistribution(values []float64) *distribution {
	copy := append([]float64(nil), values...)
	sort.Float64s(copy)
	return &distribution{Samples: len(copy), P50: percentileSorted(copy, 0.50), P95: percentileSorted(copy, 0.95), P99: percentileSorted(copy, 0.99), Max: copy[len(copy)-1]}
}

func percentile(values []float64, quantile float64) float64 {
	copy := append([]float64(nil), values...)
	sort.Float64s(copy)
	return percentileSorted(copy, quantile)
}

func percentileSorted(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(quantile*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func meanPointer(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	var total float64
	for _, value := range values {
		total += value
	}
	mean := total / float64(len(values))
	return &mean
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
