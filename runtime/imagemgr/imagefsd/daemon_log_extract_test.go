package imagefsd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestClassifyLogLine(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		expectedLevel logrus.Level
		expectMatch   bool
	}{
		{
			name:          "WARN line",
			line:          "2026-04-08T10:23:45.123456Z  WARN imagefsd::backend::dedup: Failed to update chunk access time. err=SomeError",
			expectedLevel: logrus.WarnLevel,
			expectMatch:   true,
		},
		{
			name:          "ERROR line",
			line:          "2026-04-08T10:23:45.123456Z ERROR imagefsd::fs: fuse worker exited err=SomeError",
			expectedLevel: logrus.ErrorLevel,
			expectMatch:   true,
		},
		{
			name:        "INFO line",
			line:        "2026-04-08T10:23:45.123456Z  INFO imagefsd::backend::cache: cache hit ratio 95%",
			expectMatch: false,
		},
		{
			name:        "DEBUG line",
			line:        "2026-04-08T10:23:45.123456Z DEBUG imagefsd::fs: read inode 42",
			expectMatch: false,
		},
		{
			name:        "TRACE line",
			line:        "2026-04-08T10:23:45.123456Z TRACE imagefsd::fs: entering function",
			expectMatch: false,
		},
		{
			name:        "empty line",
			line:        "",
			expectMatch: false,
		},
		{
			name:        "whitespace only",
			line:        "   ",
			expectMatch: false,
		},
		{
			name:          "WARN without microseconds",
			line:          "2026-04-08T10:23:45.1Z  WARN imagefsd::backend: short timestamp",
			expectedLevel: logrus.WarnLevel,
			expectMatch:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, msg, _ := classifyLogLine(tt.line)
			if tt.expectMatch {
				if level != tt.expectedLevel {
					t.Errorf("expected level %v, got %v", tt.expectedLevel, level)
				}
				if msg == "" {
					t.Error("expected non-empty normalized message")
				}
				// Normalized message should not contain the timestamp
				if strings.Contains(msg, "2026-04-08T") {
					t.Errorf("normalized message should not contain timestamp: %s", msg)
				}
			} else {
				if level != 0 {
					t.Errorf("expected no match (level 0), got level %v", level)
				}
			}
		})
	}
}

func TestClassifyLogLine_Deduplication(t *testing.T) {
	// Two lines with different timestamps but same message should produce the same key
	line1 := "2026-04-08T10:23:45.123456Z  WARN imagefsd::backend::dedup: Failed to update chunk access time. err=SomeError"
	line2 := "2026-04-08T10:24:00.654321Z  WARN imagefsd::backend::dedup: Failed to update chunk access time. err=SomeError"

	_, _, key1 := classifyLogLine(line1)
	_, _, key2 := classifyLogLine(line2)

	if key1 != key2 {
		t.Errorf("expected same dedup key for identical messages, got:\n  %q\n  %q", key1, key2)
	}
}

func TestClassifyLogLine_DedupStripsTrailingKV(t *testing.T) {
	// Lines differing only in trailing key=value fields should share the same dedup key
	line1 := "2026-04-08T10:23:45.000001Z  WARN imagefsd::backend::chunkdb: Failed to register chunks with chunk index. count=1"
	line2 := "2026-04-08T10:23:45.000002Z  WARN imagefsd::backend::chunkdb: Failed to register chunks with chunk index. count=8"
	line3 := "2026-04-08T10:23:45.000003Z  WARN imagefsd::backend::chunkdb: Failed to register chunks with chunk index. count=3"

	_, display1, key1 := classifyLogLine(line1)
	_, _, key2 := classifyLogLine(line2)
	_, _, key3 := classifyLogLine(line3)

	if key1 != key2 || key2 != key3 {
		t.Errorf("expected same dedup key, got:\n  %q\n  %q\n  %q", key1, key2, key3)
	}

	// Display message should still contain the original key=value
	if !strings.Contains(display1, "count=1") {
		t.Errorf("display message should preserve key=value fields: %s", display1)
	}

	// Dedup key should not contain the count= field
	if strings.Contains(key1, "count=") {
		t.Errorf("dedup key should not contain trailing key=value: %s", key1)
	}
}

func TestProcessDaemonLog(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test-daemon.log")

	content := strings.Join([]string{
		"2026-04-08T10:23:45.000001Z  INFO imagefsd::fs: starting up",
		"2026-04-08T10:23:45.000002Z  WARN imagefsd::backend::dedup: Failed to update chunk. err=timeout",
		"2026-04-08T10:23:45.000003Z  WARN imagefsd::backend::dedup: Failed to update chunk. err=timeout",
		"2026-04-08T10:23:45.000004Z  WARN imagefsd::backend::dedup: Failed to update chunk. err=timeout",
		"2026-04-08T10:23:45.000005Z ERROR imagefsd::fs: fuse worker exited err=IOError",
		"2026-04-08T10:23:45.000006Z  INFO imagefsd::fs: shutting down",
	}, "\n")

	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture logrus output
	var buf strings.Builder
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetOutput(os.Stderr)

	fields := logrus.Fields{
		"daemon_id":   "test-123",
		"daemon_name": "test-daemon",
		"mount_point": "/mnt/test",
		"source_type": "oss",
	}

	processDaemonLog(logFile, fields)

	output := buf.String()

	// Verify WARN and ERROR lines appear
	if !strings.Contains(output, "Failed to update chunk") {
		t.Error("expected WARN message in output")
	}
	if !strings.Contains(output, "fuse worker exited") {
		t.Error("expected ERROR message in output")
	}
	// Verify occurrences for deduplicated WARN
	if !strings.Contains(output, "occurrences=3") {
		t.Errorf("expected occurrences=3 for deduplicated WARN, output: %s", output)
	}
	// Verify summary line
	if !strings.Contains(output, "daemon log extraction") {
		t.Error("expected summary line in output")
	}
	// Verify staged file is cleaned up
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Error("staged log file should have been removed after processing")
	}
}

func TestProcessDaemonLog_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "empty.log")

	if err := os.WriteFile(logFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	logrus.SetOutput(&buf)
	defer logrus.SetOutput(os.Stderr)

	processDaemonLog(logFile, logrus.Fields{"daemon_id": "empty"})

	if buf.Len() > 0 {
		t.Errorf("expected no output for empty log file, got: %s", buf.String())
	}

	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Error("staged log file should have been removed")
	}
}

func TestProcessDaemonLog_MissingFile(t *testing.T) {
	var buf strings.Builder
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetOutput(os.Stderr)

	processDaemonLog("/nonexistent/path/daemon.log", logrus.Fields{"daemon_id": "missing"})

	if !strings.Contains(buf.String(), "failed to open staged daemon log") {
		t.Error("expected warning about missing file")
	}
}

func TestRescueDaemonLog(t *testing.T) {
	tmpDir := t.TempDir()
	stagingDir := filepath.Join(tmpDir, daemonLogStagingDir)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	daemonDir := filepath.Join(tmpDir, "daemons", "test-daemon-1")
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(daemonDir, "daemon.log")
	logContent := "2026-04-08T10:00:00.000000Z ERROR imagefsd::fs: test error\n"
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{
		meta: DaemonMeta{
			ID:            "test-daemon-1",
			Name:          "test",
			DaemonLogPath: logPath,
		},
	}

	var buf strings.Builder
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetOutput(os.Stderr)

	rescueDaemonLog(tmpDir, d)

	// The log file should have been moved
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Error("original log file should have been renamed away")
	}

	// Wait for goroutine to finish
	time.Sleep(100 * time.Millisecond)

	// Staged file should be cleaned up by processDaemonLog
	stagedPath := filepath.Join(stagingDir, "test-daemon-1.log")
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Error("staged log file should have been cleaned up after processing")
	}
}

func TestRescueDaemonLog_RenameFailure(t *testing.T) {
	tmpDir := t.TempDir()
	stagingDir := filepath.Join(tmpDir, daemonLogStagingDir)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{
		meta: DaemonMeta{
			ID:            "nonexistent",
			Name:          "test",
			DaemonLogPath: filepath.Join(tmpDir, "does-not-exist.log"),
		},
	}

	var buf strings.Builder
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.DebugLevel)
	defer logrus.SetOutput(os.Stderr)

	rescueDaemonLog(tmpDir, d)

	if !strings.Contains(buf.String(), "skipping daemon log extraction") {
		t.Error("expected debug message about skipped extraction")
	}
}

func TestRescueDaemonLog_EmptyPath(t *testing.T) {
	d := &Daemon{
		meta: DaemonMeta{
			ID:            "test",
			DaemonLogPath: "",
		},
	}

	// Should return immediately without error
	rescueDaemonLog("/tmp", d)
}
