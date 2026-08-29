package enforcement

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"

	"github.com/cofy-x/axern/runtime/egressd/internal/dnsforward"
	obs "github.com/cofy-x/axern/runtime/egressd/internal/observability"
	"golang.org/x/net/dns/dnsmessage"
)

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
				if err == nil {
					_, _ = listener.WriteToUDP(response, source)
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
	for _, answer := range response.Answers {
		name := canonicalDNSName(answer.Header.Name.String())
		if dnsDenied(record.GetPolicy(), name) {
			e.record(record, obs.ActionDeny, obs.ProtocolDNS, obs.ResultRefused, started)
			return refusedDNS(query)
		}
		switch body := answer.Body.(type) {
		case *dnsmessage.CNAMEResource:
			if dnsDenied(record.GetPolicy(), canonicalDNSName(body.CNAME.String())) {
				e.record(record, obs.ActionDeny, obs.ProtocolDNS, obs.ResultRefused, started)
				return refusedDNS(query)
			}
		case *dnsmessage.AResource:
			addr := netip.AddrFrom4(body.A)
			if record.GetPolicy().GetStrict() != nil && domainAllowed(record.GetPolicy(), name) && eligibleDomainAddress(addr) {
				e.authorize(source, name, addr, answer.Header.TTL)
			}
		case *dnsmessage.AAAAResource:
			addr := netip.AddrFrom16(body.AAAA)
			if record.GetPolicy().GetStrict() != nil && domainAllowed(record.GetPolicy(), name) && eligibleDomainAddress(addr) {
				e.authorize(source, name, addr, answer.Header.TTL)
			}
		}
	}
	e.record(record, obs.ActionAllow, obs.ProtocolDNS, obs.ResultOK, started)
	return responseWire, nil
}

func eligibleDomainAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !addr.IsMulticast() && !addr.IsUnspecified()
}

func canonicalDNSName(value string) string {
	if len(value) > 0 && value[len(value)-1] == '.' {
		return value[:len(value)-1]
	}
	return value
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
