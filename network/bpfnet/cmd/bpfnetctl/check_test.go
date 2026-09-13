package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/network/bpfnet/internal/inspect"
)

func TestEvaluateReadinessRejectsMissingPinnedPrograms(t *testing.T) {
	result := evaluateReadiness(bpfnet.Status{
		State: bpfnet.DataplaneState{
			TCReady: true,
		},
		Attachment: bpfnet.AttachmentReadiness{
			IngressTCAttached: true,
			EgressTCAttached:  true,
			PinnedMapsReady:   true,
		},
	}, nil)

	if result.OK {
		t.Fatalf("expected missing pinned programs to fail readiness: %#v", result)
	}
	var buf bytes.Buffer
	writeCheckResult(&buf, result)
	if !strings.Contains(buf.String(), "fail pinned_programs") {
		t.Fatalf("expected pinned program failure in output:\n%s", buf.String())
	}
}

func TestEvaluateReadinessAcceptsReadyObjects(t *testing.T) {
	result := evaluateReadiness(bpfnet.Status{
		State: bpfnet.DataplaneState{
			TCReady:            true,
			LocalhostTCPDNAT:   true,
			LocalhostPathReady: true,
		},
		Attachment: bpfnet.AttachmentReadiness{
			IngressTCAttached:      true,
			EgressTCAttached:       true,
			LocalhostLinksAttached: true,
			PinnedMapsReady:        true,
			PinnedProgramsReady:    true,
		},
	}, []inspect.ObjectInfo{
		{Kind: "map", Name: "service_map", Present: true, Openable: true},
		{Kind: "program", Name: "ingress", Present: true, Openable: true},
		{Kind: "link", Name: "localhost-connect4", Present: true, Openable: true},
	})

	if !result.OK {
		t.Fatalf("expected ready status and objects to pass: %#v", result)
	}
}

func TestEvaluateReadinessRejectsMissingLocalhostPath(t *testing.T) {
	result := evaluateReadiness(bpfnet.Status{
		State: bpfnet.DataplaneState{
			TCReady: true,
		},
		Attachment: bpfnet.AttachmentReadiness{
			IngressTCAttached:   true,
			EgressTCAttached:    true,
			PinnedMapsReady:     true,
			PinnedProgramsReady: true,
		},
	}, []inspect.ObjectInfo{
		{Kind: "map", Name: "service_map", Present: true, Openable: true},
		{Kind: "program", Name: "ingress", Present: true, Openable: true},
		{Kind: "link", Name: "localhost-connect4", Present: false, Openable: false},
	})

	if result.OK {
		t.Fatalf("expected missing localhost path to fail readiness: %#v", result)
	}
	var buf bytes.Buffer
	writeCheckResult(&buf, result)
	if !strings.Contains(buf.String(), "fail localhost_path") {
		t.Fatalf("expected localhost path failure in output:\n%s", buf.String())
	}
}

func TestEvaluateReadinessReportsBrokenLinks(t *testing.T) {
	result := evaluateReadiness(bpfnet.Status{
		State: bpfnet.DataplaneState{
			TCReady:            true,
			LocalhostTCPDNAT:   true,
			LocalhostPathReady: true,
		},
		Attachment: bpfnet.AttachmentReadiness{
			IngressTCAttached:   true,
			EgressTCAttached:    true,
			PinnedMapsReady:     true,
			PinnedProgramsReady: true,
		},
	}, []inspect.ObjectInfo{
		{Kind: "map", Name: "service_map", Present: true, Openable: true},
		{Kind: "program", Name: "ingress", Present: true, Openable: true},
		{Kind: "link", Name: "egress", Present: false, Openable: false},
	})

	if result.OK {
		t.Fatalf("expected non-localhost link failure to remain visible: %#v", result)
	}
}
