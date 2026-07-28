package rollout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

// WorkspaceBaseline captures the git state of the workspace immediately
// after initial upload, before the agent runs. Non-git workspaces produce
// a nil baseline (valid for non-git tasks).
type WorkspaceBaseline struct {
	Revision   string           `json:"revision,omitempty"`
	Clean      bool             `json:"clean"`
	Untracked  []UntrackedEntry `json:"untracked,omitempty"`
	DirtyFiles []string         `json:"dirty_files,omitempty"`
}

// UntrackedEntry records a file present in the workspace at baseline
// that is not tracked by git. The ContentHash is the SHA-256 of the
// file contents, used to exclude pre-existing files from agent patches.
type UntrackedEntry struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}

func captureWorkspaceBaseline(ctx context.Context, instance sandbox.Instance, workdir string) (*WorkspaceBaseline, error) {
	if workdir == "" {
		workdir = "/workspace"
	}
	opts := sandbox.ExecOptions{CWD: workdir}

	revResult, err := instance.Exec(ctx, sandbox.ShellCommand("git rev-parse HEAD"), opts)
	if err != nil {
		return nil, nil
	}
	if revResult.ExitCode != 0 {
		return nil, nil
	}
	revision := strings.TrimSpace(revResult.Stdout)
	if revision == "" {
		return nil, nil
	}

	baseline := &WorkspaceBaseline{
		Revision: revision,
		Clean:    true,
	}

	statusResult, err := instance.Exec(ctx, sandbox.ShellCommand("git status --porcelain"), opts)
	if err != nil {
		return baseline, nil
	}
	if statusResult.ExitCode != 0 {
		return baseline, nil
	}

	statusOutput := strings.TrimSpace(statusResult.Stdout)
	if statusOutput == "" {
		return baseline, nil
	}

	baseline.Clean = false
	for _, line := range strings.Split(statusOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "??"))
			baseline.Untracked = append(baseline.Untracked, UntrackedEntry{Path: path})
		} else if len(line) > 3 {
			baseline.DirtyFiles = append(baseline.DirtyFiles, strings.TrimSpace(line[3:]))
		}
	}

	return baseline, nil
}

// captureUntrackedHashes fills in content hashes for each untracked file
// using git hash-object. Errors on individual files are silently skipped.
func captureUntrackedHashes(ctx context.Context, instance sandbox.Instance, workdir string, baseline *WorkspaceBaseline) {
	if baseline == nil || len(baseline.Untracked) == 0 {
		return
	}
	if workdir == "" {
		workdir = "/workspace"
	}
	opts := sandbox.ExecOptions{CWD: workdir}
	for i, entry := range baseline.Untracked {
		hashResult, err := instance.Exec(ctx, sandbox.ShellCommand(
			fmt.Sprintf("sha256sum %s 2>/dev/null | cut -d' ' -f1", shellQuote(entry.Path)),
		), opts)
		if err != nil || hashResult.ExitCode != 0 {
			continue
		}
		hash := strings.TrimSpace(hashResult.Stdout)
		if isValidSHA256(hash) {
			baseline.Untracked[i].ContentHash = hash
		}
	}
}

func isValidSHA256(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func baselineMetadata(b *WorkspaceBaseline) domain.KeyValue {
	if b == nil {
		return nil
	}
	m := domain.KeyValue{
		"revision": b.Revision,
		"clean":    fmt.Sprintf("%t", b.Clean),
	}
	if len(b.Untracked) > 0 {
		m["untracked_count"] = fmt.Sprintf("%d", len(b.Untracked))
	}
	if len(b.DirtyFiles) > 0 {
		m["dirty_count"] = fmt.Sprintf("%d", len(b.DirtyFiles))
	}
	return m
}
