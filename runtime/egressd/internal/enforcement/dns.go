package enforcement

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/egressd/internal/dnsforward"
	obs "github.com/cofy-x/axern/runtime/egressd/internal/observability"
	"golang.org/x/net/dns/dnsmessage"
)

const maxCNAMEChainDepth = 16

type dnsAddressAnswer struct {
	addr netip.Addr
	ttl  uint32
	kind dnsmessage.Type
}

type dnsCNAMEAnswer struct {
	target string
	ttl    uint32
}

func (e *Engine) serveDNSUDP(ctxDone interface{ Done() <-chan struct{} }, listener *net.UDPConn) {
	buffer := make([]byte, dnsforward.MaxDNSMessageBytes)
	for {
		n, source, err := listener.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-ctxDone.Done():
				return
			default:
				continue
			}
		}
		query := append([]byte(nil), buffer[:n]...)
		select {
		case e.sem <- struct{}{}:
			go func() {
				defer func() { <-e.sem }()
				response, err := e.resolveDNS(sourceIP(source), query, false)
				if err != nil {
					e.dnsResolveFailureOnce.Do(func() { fmt.Fprintln(os.Stderr, "egressd_dns_udp_resolve_failure=true") })
					return
				}
				_ = listener.SetWriteDeadline(time.Now().Add(inspectTimeout))
				if _, err := listener.WriteToUDP(response, source); err != nil {
					e.dnsResponseFailureOnce.Do(func() { fmt.Fprintln(os.Stderr, "egressd_dns_udp_response_failure=true") })
				}
			}()
		default:
		}
	}
}

func (e *Engine) handleDNSTCP(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(inspectTimeout))
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return
	}
	length := int(binary.BigEndian.Uint16(size[:]))
	if length == 0 {
		return
	}
	query := make([]byte, length)
	if _, err := io.ReadFull(conn, query); err != nil {
		return
	}
	response, err := e.resolveDNS(sourceIP(conn.RemoteAddr()), query, true)
	if err != nil || len(response) > 65535 {
		return
	}
	binary.BigEndian.PutUint16(size[:], uint16(len(response)))
	_, _ = conn.Write(append(size[:], response...))
}

func (e *Engine) resolveDNS(source string, wire []byte, tcp bool) ([]byte, error) {
	started := time.Now()
	record := e.policy(source)
	if record == nil {
		return nil, fmt.Errorf("no policy for source")
	}
	var query dnsmessage.Message
	if err := query.Unpack(wire); err != nil || query.Header.Response || len(query.Questions) == 0 {
		return nil, fmt.Errorf("invalid DNS query")
	}
	for _, question := range query.Questions {
		name := canonicalDNSName(question.Name.String())
		if dnsDenied(record.GetPolicy(), name) {
			e.record(record, obs.ActionDeny, obs.ProtocolDNS, obs.ResultRefused, started)
			return refusedDNS(query)
		}
	}
	upstreams, err := dnsforward.ParseUpstreams(record.GetUpstreamNameservers())
	if err != nil {
		return nil, err
	}
	responseWire, err := exchangeDNS(wire, upstreams[0], tcp)
	if err != nil {
		return nil, err
	}
	var response dnsmessage.Message
	if err := response.Unpack(responseWire); err != nil {
		return nil, fmt.Errorf("invalid upstream DNS response: %w", err)
	}
	if err := validateDNSResponse(query, response); err != nil {
		return nil, err
	}
	if record.GetPolicy().GetDnsDeny() != nil {
		for _, answer := range dnsResources(response) {
			name := canonicalDNSName(answer.Header.Name.String())
			if dnsDenied(record.GetPolicy(), name) {
				e.record(record, obs.ActionDeny, obs.ProtocolDNS, obs.ResultRefused, started)
				return refusedDNS(query)
			}
			if body, ok := answer.Body.(*dnsmessage.CNAMEResource); ok && dnsDenied(record.GetPolicy(), canonicalDNSName(body.CNAME.String())) {
				e.record(record, obs.ActionDeny, obs.ProtocolDNS, obs.ResultRefused, started)
				return refusedDNS(query)
			}
		}
	}
	if record.GetPolicy().GetStrict() != nil && !response.Header.Truncated && response.Header.RCode == dnsmessage.RCodeSuccess {
		authorizations, err := strictDNSAuthorizations(query.Questions, response.Answers)
		if err != nil {
			return nil, fmt.Errorf("invalid strict DNS answer chain: %w", err)
		}
		for domain, answers := range authorizations {
			for _, answer := range answers {
				if eligibleDomainAddress(answer.addr) {
					e.authorize(source, domain, answer.addr, answer.ttl)
				}
			}
		}
	}
	e.record(record, obs.ActionAllow, obs.ProtocolDNS, obs.ResultOK, started)
	return responseWire, nil
}

func validateDNSResponse(query, response dnsmessage.Message) error {
	if !response.Header.Response || response.Header.ID != query.Header.ID || response.Header.OpCode != query.Header.OpCode {
		return fmt.Errorf("upstream DNS response header does not match query")
	}
	if len(response.Questions) != len(query.Questions) {
		return fmt.Errorf("upstream DNS response question count does not match query")
	}
	for index := range query.Questions {
		got, want := response.Questions[index], query.Questions[index]
		if canonicalDNSName(got.Name.String()) != canonicalDNSName(want.Name.String()) || got.Type != want.Type || got.Class != want.Class {
			return fmt.Errorf("upstream DNS response question %d does not match query", index)
		}
	}
	return nil
}

func dnsResources(message dnsmessage.Message) []dnsmessage.Resource {
	resources := make([]dnsmessage.Resource, 0, len(message.Answers)+len(message.Authorities)+len(message.Additionals))
	resources = append(resources, message.Answers...)
	resources = append(resources, message.Authorities...)
	resources = append(resources, message.Additionals...)
	return resources
}

func strictDNSAuthorizations(questions []dnsmessage.Question, answers []dnsmessage.Resource) (map[string][]dnsAddressAnswer, error) {
	cnames := make(map[string]dnsCNAMEAnswer)
	addresses := make(map[string][]dnsAddressAnswer)
	for _, answer := range answers {
		name := canonicalDNSName(answer.Header.Name.String())
		switch body := answer.Body.(type) {
		case *dnsmessage.CNAMEResource:
			if _, exists := cnames[name]; exists {
				return nil, fmt.Errorf("multiple CNAME answers for %s", name)
			}
			cnames[name] = dnsCNAMEAnswer{target: canonicalDNSName(body.CNAME.String()), ttl: answer.Header.TTL}
		case *dnsmessage.AResource:
			addresses[name] = append(addresses[name], dnsAddressAnswer{addr: netip.AddrFrom4(body.A), ttl: answer.Header.TTL, kind: dnsmessage.TypeA})
		case *dnsmessage.AAAAResource:
			addresses[name] = append(addresses[name], dnsAddressAnswer{addr: netip.AddrFrom16(body.AAAA), ttl: answer.Header.TTL, kind: dnsmessage.TypeAAAA})
		}
	}
	for owner := range cnames {
		if len(addresses[owner]) != 0 {
			return nil, fmt.Errorf("CNAME owner %s also has address data", owner)
		}
	}

	result := make(map[string][]dnsAddressAnswer)
	for _, question := range questions {
		root := canonicalDNSName(question.Name.String())
		current := root
		ttl := ^uint32(0)
		visited := map[string]struct{}{current: {}}
		for depth := 0; ; depth++ {
			link, ok := cnames[current]
			if !ok {
				break
			}
			if depth >= maxCNAMEChainDepth {
				return nil, fmt.Errorf("CNAME chain for %s exceeds %d links", root, maxCNAMEChainDepth)
			}
			if link.ttl < ttl {
				ttl = link.ttl
			}
			current = link.target
			if _, exists := visited[current]; exists {
				return nil, fmt.Errorf("CNAME chain for %s contains a loop", root)
			}
			visited[current] = struct{}{}
		}
		for _, answer := range addresses[current] {
			if question.Type != dnsmessage.TypeALL && answer.kind != question.Type {
				continue
			}
			effectiveTTL := answer.ttl
			if ttl < effectiveTTL {
				effectiveTTL = ttl
			}
			answer.ttl = effectiveTTL
			result[root] = append(result[root], answer)
		}
	}
	return result, nil
}

func eligibleDomainAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !addr.IsMulticast() && !addr.IsUnspecified()
}

func canonicalDNSName(value string) string {
	return strings.ToLower(strings.TrimSuffix(value, "."))
}

func refusedDNS(query dnsmessage.Message) ([]byte, error) {
	response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true, OpCode: query.Header.OpCode, RecursionDesired: query.Header.RecursionDesired, RCode: dnsmessage.RCodeRefused}, Questions: query.Questions}
	return response.Pack()
}

func exchangeDNS(query []byte, upstream netip.AddrPort, tcp bool) ([]byte, error) {
	network := "udp"
	if tcp {
		network = "tcp"
	}
	conn, err := net.DialTimeout(network, upstream.String(), inspectTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(inspectTimeout))
	if tcp {
		if len(query) > 65535 {
			return nil, fmt.Errorf("DNS query too large")
		}
		wire := make([]byte, len(query)+2)
		binary.BigEndian.PutUint16(wire[:2], uint16(len(query)))
		copy(wire[2:], query)
		if _, err := conn.Write(wire); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(conn, wire[:2]); err != nil {
			return nil, err
		}
		length := int(binary.BigEndian.Uint16(wire[:2]))
		response := make([]byte, length)
		_, err = io.ReadFull(conn, response)
		return response, err
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	response := make([]byte, dnsforward.MaxDNSMessageBytes)
	n, err := conn.Read(response)
	return response[:n], err
}
