package enforcement

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/egressd/internal/dnsforward"
	"github.com/cofy-x/axern/runtime/egressd/internal/l7inspect"
	obs "github.com/cofy-x/axern/runtime/egressd/internal/observability"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

const (
	dnsProxyPort   = 1053
	httpProxyPort  = 1080
	httpsProxyPort = 1443
	maxTTL         = 10 * time.Minute
	negativeTTL    = 30 * time.Second
	inspectTimeout = 5 * time.Second
	maxConcurrent  = 512
)

type Engine struct {
	mu       sync.RWMutex
	policies map[string]*runtimeegressv1.PreparedEgressPolicy
	auth     map[string]map[netip.Addr][]authorization
	sem      chan struct{}
	servers  []io.Closer
	metrics  *obs.Metrics
}

func NewEngine() *Engine {
	metrics, _ := obs.NewMetrics(nil)
	return &Engine{policies: map[string]*runtimeegressv1.PreparedEgressPolicy{}, auth: map[string]map[netip.Addr][]authorization{}, sem: make(chan struct{}, maxConcurrent), metrics: metrics}
}

func (e *Engine) SetPolicies(records []*runtimeegressv1.PreparedEgressPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	next := make(map[string]*runtimeegressv1.PreparedEgressPolicy, len(records))
	for _, record := range records {
		if record != nil {
			next[normalizedSource(record.GetSandboxIp())] = record
		}
	}
	for source := range e.auth {
		current := e.policies[source]
		desired := next[source]
		if desired == nil || current == nil || current.GetAllocationID() != desired.GetAllocationID() || current.GetAttempt() != desired.GetAttempt() || current.GetPolicyDigest() != desired.GetPolicyDigest() {
			delete(e.auth, source)
		}
	}
	e.policies = next
}

func (e *Engine) policy(source string) *runtimeegressv1.PreparedEgressPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.policies[normalizedSource(source)]
}

func (e *Engine) authorize(source, domain string, addr netip.Addr, ttl uint32) {
	if ttl == 0 {
		return
	}
	duration := time.Duration(ttl) * time.Second
	if duration > maxTTL {
		duration = maxTTL
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	source = normalizedSource(source)
	if e.auth[source] == nil {
		e.auth[source] = map[netip.Addr][]authorization{}
	}
	e.auth[source][addr.Unmap()] = append(e.auth[source][addr.Unmap()], authorization{domain: domain, expiry: time.Now().Add(duration)})
}

func (e *Engine) authorized(source, domain string, addr netip.Addr) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	source = normalizedSource(source)
	byAddress := e.auth[source]
	if byAddress == nil {
		return false
	}
	entries := byAddress[addr.Unmap()]
	kept := entries[:0]
	allowed := false
	for _, entry := range entries {
		if now.Before(entry.expiry) {
			kept = append(kept, entry)
			allowed = allowed || entry.valid(domain, now)
		}
	}
	byAddress[addr.Unmap()] = kept
	return allowed
}

func (e *Engine) Start(ctx context.Context) error {
	udp, err := listenTransparentUDP(dnsProxyPort)
	if err != nil {
		return fmt.Errorf("listen DNS UDP: %w", err)
	}
	dnsTCP, err := listenTransparentTCP(dnsProxyPort)
	if err != nil {
		_ = udp.Close()
		return fmt.Errorf("listen DNS TCP: %w", err)
	}
	http, err := listenTransparentTCP(httpProxyPort)
	if err != nil {
		_ = udp.Close()
		_ = dnsTCP.Close()
		return fmt.Errorf("listen HTTP proxy: %w", err)
	}
	https, err := listenTransparentTCP(httpsProxyPort)
	if err != nil {
		_ = udp.Close()
		_ = dnsTCP.Close()
		_ = http.Close()
		return fmt.Errorf("listen HTTPS proxy: %w", err)
	}
	e.servers = []io.Closer{udp, dnsTCP, http, https}
	go e.serveDNSUDP(ctx, udp)
	go e.serveTCP(ctx, dnsTCP, e.handleDNSTCP)
	go e.serveTCP(ctx, http, e.handleHTTP)
	go e.serveTCP(ctx, https, e.handleTLS)
	return nil
}

func (e *Engine) Close() error {
	var result error
	for _, server := range e.servers {
		result = errors.Join(result, server.Close())
	}
	return result
}

func (e *Engine) serveTCP(ctx context.Context, listener net.Listener, handler func(net.Conn)) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		select {
		case e.sem <- struct{}{}:
			go func() { defer func() { <-e.sem }(); handler(conn) }()
		default:
			_ = conn.Close()
		}
	}
}

func sourceIP(addr net.Addr) string {
	switch value := addr.(type) {
	case *net.TCPAddr:
		return value.AddrPort().Addr().Unmap().String()
	case *net.UDPAddr:
		return value.AddrPort().Addr().Unmap().String()
	default:
		return ""
	}
}

func (e *Engine) handleHTTP(conn net.Conn) {
	started := time.Now()
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(inspectTimeout))
	request, err := l7inspect.ReadHTTPRequest(conn, l7inspect.DefaultMaxHTTPHeaderBytes)
	if err != nil || request.DirectIP {
		return
	}
	record := e.policy(sourceIP(conn.RemoteAddr()))
	if record == nil || !domainAllowed(record.GetPolicy(), request.Host) {
		e.record(record, obs.ActionDeny, obs.ProtocolHTTP, obs.ResultRefused, started)
		return
	}
	destination, err := originalDestination(conn)
	if err != nil || !e.authorized(sourceIP(conn.RemoteAddr()), request.Host, destination.Addr()) {
		e.record(record, obs.ActionDeny, obs.ProtocolHTTP, obs.ResultRefused, started)
		return
	}
	upstream, err := dialMarked(destination)
	if err != nil {
		return
	}
	defer upstream.Close()
	_ = conn.SetReadDeadline(time.Time{})
	if _, err := upstream.Write(request.Bytes); err != nil {
		return
	}
	e.record(record, obs.ActionAllow, obs.ProtocolHTTP, obs.ResultOK, started)
	proxyBoth(conn, upstream)
}

func (e *Engine) handleTLS(conn net.Conn) {
	started := time.Now()
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(inspectTimeout))
	prefix := make([]byte, 0, l7inspect.DefaultMaxClientHelloBytes)
	chunk := make([]byte, 4096)
	var hello l7inspect.ClientHello
	for len(prefix) < l7inspect.DefaultMaxClientHelloBytes {
		n, err := conn.Read(chunk)
		if n > 0 {
			prefix = append(prefix, chunk[:n]...)
		}
		hello, err = l7inspect.ParseClientHello(prefix, l7inspect.DefaultMaxClientHelloBytes)
		if err == nil {
			break
		}
		if err != nil && n == 0 {
			return
		}
	}
	if hello.HasECH || hello.ServerName == "" {
		return
	}
	record := e.policy(sourceIP(conn.RemoteAddr()))
	if record == nil || !domainAllowed(record.GetPolicy(), hello.ServerName) {
		e.record(record, obs.ActionDeny, obs.ProtocolHTTPS, obs.ResultRefused, started)
		return
	}
	destination, err := originalDestination(conn)
	if err != nil || !e.authorized(sourceIP(conn.RemoteAddr()), hello.ServerName, destination.Addr()) {
		e.record(record, obs.ActionDeny, obs.ProtocolHTTPS, obs.ResultRefused, started)
		return
	}
	upstream, err := dialMarked(destination)
	if err != nil {
		return
	}
	defer upstream.Close()
	_ = conn.SetReadDeadline(time.Time{})
	if _, err := upstream.Write(prefix); err != nil {
		return
	}
	e.record(record, obs.ActionAllow, obs.ProtocolHTTPS, obs.ResultOK, started)
	proxyBoth(conn, upstream)
}

func proxyBoth(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); done <- struct{}{} }()
	<-done
}

var _ = dnsforward.MaxDNSMessageBytes
var _ = negativeTTL

func (e *Engine) record(record *runtimeegressv1.PreparedEgressPolicy, action obs.Action, protocol obs.Protocol, result obs.Result, started time.Time) {
	if record == nil {
		return
	}
	mode := obs.ModeDNSDeny
	rules := int64(len(record.GetPolicy().GetDnsDeny().GetDeniedDomains()))
	if strict := record.GetPolicy().GetStrict(); strict != nil {
		mode = obs.ModeStrict
		rules = int64(len(strict.GetAllowedDomains()) + len(strict.GetAllowedCidrs()))
	}
	e.metrics.Record(context.Background(), obs.Event{Mode: mode, Action: action, Protocol: protocol, Result: result, AllocationID: record.GetAllocationID(), RuleCount: rules, Latency: time.Since(started)})
}
