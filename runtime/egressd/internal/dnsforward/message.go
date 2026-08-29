package dnsforward

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/lib/go/networkpolicy"
	"golang.org/x/net/dns/dnsmessage"
)

const MaxDNSMessageBytes = 65535

type Message struct {
	Header    dnsmessage.Header
	Questions []dnsmessage.Question
	CNAMEs    []CNAME
}

type CNAME struct {
	Name   string
	Target string
}

func InspectMessage(wire []byte) (Message, error) {
	if len(wire) == 0 || len(wire) > MaxDNSMessageBytes {
		return Message{}, fmt.Errorf("DNS message size must be 1..%d bytes", MaxDNSMessageBytes)
	}
	var parser dnsmessage.Parser
	header, err := parser.Start(wire)
	if err != nil {
		return Message{}, fmt.Errorf("parse DNS header: %w", err)
	}
	out := Message{Header: header}
	for {
		question, err := parser.Question()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return Message{}, fmt.Errorf("parse DNS question: %w", err)
		}
		name, err := normalizeDNSName(question.Name.String())
		if err != nil {
			return Message{}, fmt.Errorf("parse DNS question name: %w", err)
		}
		question.Name, err = dnsmessage.NewName(name + ".")
		if err != nil {
			return Message{}, fmt.Errorf("canonicalize DNS question name: %w", err)
		}
		out.Questions = append(out.Questions, question)
	}
	sections := []struct {
		name   string
		header func() (dnsmessage.ResourceHeader, error)
		skip   func() error
	}{
		{name: "answer", header: parser.AnswerHeader, skip: parser.SkipAnswer},
		{name: "authority", header: parser.AuthorityHeader, skip: parser.SkipAuthority},
		{name: "additional", header: parser.AdditionalHeader, skip: parser.SkipAdditional},
	}
	for _, section := range sections {
		cnames, err := inspectSection(section.name, section.header, section.skip, &parser)
		if err != nil {
			return Message{}, err
		}
		out.CNAMEs = append(out.CNAMEs, cnames...)
	}
	return out, nil
}

func inspectSection(section string, nextHeader func() (dnsmessage.ResourceHeader, error), skip func() error, parser *dnsmessage.Parser) ([]CNAME, error) {
	var out []CNAME
	for {
		header, err := nextHeader()
		if err == dnsmessage.ErrSectionDone {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse DNS %s resource header: %w", section, err)
		}
		if header.Type != dnsmessage.TypeCNAME {
			if err := skip(); err != nil {
				return nil, fmt.Errorf("skip DNS %s resource: %w", section, err)
			}
			continue
		}
		resource, err := parser.CNAMEResource()
		if err != nil {
			return nil, fmt.Errorf("parse DNS %s CNAME: %w", section, err)
		}
		name, err := normalizeDNSName(header.Name.String())
		if err != nil {
			return nil, err
		}
		target, err := normalizeDNSName(resource.CNAME.String())
		if err != nil {
			return nil, err
		}
		out = append(out, CNAME{Name: name, Target: target})
	}
}

func normalizeDNSName(value string) (string, error) {
	value = strings.TrimSuffix(value, ".")
	if value == "" {
		return "", nil
	}
	return networkpolicy.NormalizeDomain(value)
}
