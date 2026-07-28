package timedtrace

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCanonicalPhaseForStage(t *testing.T) {
	tests := []struct {
		stage string
		want  string
	}{
		{stage: "reuse_in_memory_mount", want: "rootfs_prepare"},
		{stage: "create_daemon", want: "rootfs_prepare"},
		{stage: "wait_mount_ready", want: "rootfs_prepare"},
		{stage: "bootstrap_cache_lookup", want: "rootfs_prepare"},
		{stage: "bootstrap_cache_store", want: "rootfs_prepare"},
		{stage: "read_image_config", want: "rootfs_prepare"},
		{stage: "open_bootstrap_stream", want: "rootfs_prepare"},
		{stage: "scan_bootstrap_archive", want: "rootfs_prepare"},
		{stage: "copy_bootstrap_file", want: "rootfs_prepare"},
		{stage: "validate_request", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.stage, func(t *testing.T) {
			if got := canonicalPhaseForStage(tt.stage); got != tt.want {
				t.Fatalf("canonicalPhaseForStage(%q) = %q, want %q", tt.stage, got, tt.want)
			}
		})
	}
}

func TestBuildSummaryIncludesCanonicalPhaseLabels(t *testing.T) {
	op := &Operation{
		operation:       "api.MountNydus",
		identifierKey:   "identifier",
		identifierValue: "daemon-1",
		stages: []stageRecord{
			{name: "check_existing_daemon", canonicalPhase: canonicalPhaseForStage("check_existing_daemon"), duration: 5 * time.Millisecond},
			{name: "validate_request", canonicalPhase: canonicalPhaseForStage("validate_request"), duration: time.Millisecond},
		},
	}

	summary := op.buildSummary(20 * time.Millisecond)
	if !strings.Contains(summary, "check_existing_daemon[rootfs_prepare]") {
		t.Fatalf("summary missing canonical phase label: %s", summary)
	}
	if !strings.Contains(summary, "validate_request=") {
		t.Fatalf("summary missing raw stage label: %s", summary)
	}
}

func TestFailDoesNotEndOperationAndEndIsIdempotent(t *testing.T) {
	op, _ := Start(t.Context(), Config{Operation: "test.operation"})
	wantErr := fmt.Errorf("failed")

	op.Fail(wantErr)
	if op.err != wantErr {
		t.Fatalf("Fail() error = %v, want %v", op.err, wantErr)
	}
	if op.ended {
		t.Fatal("Fail() ended operation")
	}

	op.End()
	if !op.ended {
		t.Fatal("End() did not mark operation ended")
	}
	op.End()
}
