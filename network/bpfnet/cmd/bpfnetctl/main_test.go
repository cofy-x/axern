package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpCommandShowsSubcommandOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"help", "dump"}, &stdout, &stderr); err != nil {
		t.Fatalf("help dump: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"usage: bpfnetctl dump",
		"--pin-path PATH",
		"--raw",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected help output to contain %q, got:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestNoArgsShowsUsageWithoutError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(nil, &stdout, &stderr); err != nil {
		t.Fatalf("no args: %v", err)
	}
	if !strings.Contains(stderr.String(), "usage: bpfnetctl") {
		t.Fatalf("expected usage on stderr, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "missing command") {
		t.Fatalf("unexpected missing command noise:\n%s", stderr.String())
	}
}

func TestFlagHelpReturnsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"status", "-h"}, &stdout, &stderr); err != nil {
		t.Fatalf("status -h: %v", err)
	}
	if !strings.Contains(stdout.String(), "usage: bpfnetctl status") {
		t.Fatalf("unexpected help output:\n%s", stdout.String())
	}
}

func TestDumpAcceptsFlagsBeforeMapName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"dump", "--pin-path", "/tmp/missing", "service_map"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "open pinned map") {
		t.Fatalf("expected pinned map open error, got %v", err)
	}
}
