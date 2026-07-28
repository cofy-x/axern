package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewRolloutRunIDUsesRunPrefix(t *testing.T) {
	id, err := NewRolloutRunID(time.Date(2026, 5, 18, 12, 34, 56, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewRolloutRunID returned error: %v", err)
	}
	if !strings.HasPrefix(id, "run_20260518T123456Z_") {
		t.Fatalf("id = %q, want timestamped run_ prefix", id)
	}
}

func TestNewEpisodeIDUsesRunTaskAndAttempt(t *testing.T) {
	id := NewEpisodeID("test-run", "smoke-task", 1)
	if id != "episode_test-run_smoke-task_1" {
		t.Fatalf("id = %q", id)
	}
}
