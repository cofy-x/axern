package rollout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

type patchCaptureRequest struct {
	ctx      context.Context
	store    Store
	paths    Paths
	sandbox  sandbox.Instance
	agent    domain.AgentSpec
	baseline *WorkspaceBaseline
}

func captureAgentPatch(req patchCaptureRequest, result *domain.AgentResult) error {
	if result == nil || result.Status != domain.AgentStatusCompleted || req.paths.ArtifactDir == "" {
		return nil
	}
	policy := agentArtifactPolicy(req.agent)
	if policy == nil || strings.TrimSpace(policy.PatchPath) == "" {
		return nil
	}
	patchPath := strings.TrimSpace(policy.PatchPath)
	workdir := resolveAgentWorkdir(req.agent)

	var excludePaths []string
	if req.baseline != nil {
		excludePaths = excludeBaselineDirtiness(req.ctx, req.sandbox, workdir, req.baseline)
	}

	patch, err := readRemoteFileIfExists(req.ctx, req.sandbox, patchPath)
	if err != nil {
		return err
	}

	source := "file"
	if strings.TrimSpace(patch) == "" {
		patch, err = extractGitDiff(req.ctx, req.sandbox, workdir, req.baseline, excludePaths)
		if err != nil {
			return err
		}
		if strings.TrimSpace(patch) != "" {
			source = "git_diff"
		}
	}

	if strings.TrimSpace(patch) == "" {
		if policy.PatchRequired {
			markAgentMissingRequiredPatch(result, patchPath)
		}
		return nil
	}

	patch = ensureTrailingNewline(patch)

	validation := validatePatchFormat(patch, source)
	result.PatchValidation = &validation

	if !validation.Valid {
		return nil
	}

	ref, err := req.store.WriteAgentArtifact(req.paths.ArtifactDir, "agent.patch", patch)
	if err != nil {
		return err
	}
	result.PatchRef = ref
	result.Artifacts = appendArtifact(result.Artifacts, artifactForPath(ref, domain.ArtifactKindPatch, "agent patch"))
	if err := reconcileAgentPatch(req.ctx, req.sandbox, workdir, patchPath, policy.PatchRequired); err != nil {
		return err
	}

	timestamp := time.Now().UTC()
	rawRef, err := req.store.AppendAgentRawEvent(req.paths.ArtifactDir, domain.AgentRawEvent{
		Timestamp:    &timestamp,
		Type:         domain.AgentRawEventPatch,
		PatchRef:     ref,
		ArtifactRef:  ref,
		ArtifactKind: domain.ArtifactKindPatch,
	})
	if err != nil {
		return err
	}
	if result.RawLogRef == "" {
		result.RawLogRef = rawRef
	}
	return nil
}

func reconcileAgentPatch(ctx context.Context, instance sandbox.Instance, workdir string, patchPath string, required bool) error {
	if strings.TrimSpace(workdir) == "" || strings.TrimSpace(patchPath) == "" {
		return nil
	}
	quotedPatch := shellQuote(patchPath)
	command := fmt.Sprintf(
		"if git apply --reverse --check %[1]s >/dev/null 2>&1; then exit 0; fi; git apply --check %[1]s && git apply %[1]s",
		quotedPatch,
	)
	result, err := instance.Exec(ctx, sandbox.ShellCommand(command), sandbox.ExecOptions{CWD: workdir})
	if err != nil {
		if required {
			return fmt.Errorf("reconcile required agent patch: %w", err)
		}
		return nil
	}
	if result.ExitCode != 0 && required {
		return fmt.Errorf("reconcile required agent patch exited with status %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// validatePatchFormat checks structural validity of a captured patch.
//
// We do NOT run `git apply --check` for either source. For "git_diff" patches,
// git itself produced valid output. For "file" patches written by the agent,
// the workspace already contains the agent's modifications, so the patch
// cannot be re-applied on top of the current state — `git apply --check`
// would always fail. Structural validation (diff headers, hunk markers)
// provides the meaningful signal.
func validatePatchFormat(patch string, source string) domain.PatchValidation {
	pv := domain.PatchValidation{
		Source:   source,
		Valid:    true,
		ByteSize: int64(len(patch)),
		SHA256:   patchSHA256(patch),
	}

	if !strings.Contains(patch, "diff --git") && !strings.Contains(patch, "diff -") {
		pv.Valid = false
		pv.Error = "patch does not contain diff headers"
		return pv
	}

	pv.HunkCount = strings.Count(patch, "\n@@")
	pv.FilesChanged = countDiffHeaders(patch)
	pv.ApplyCheck = true

	return pv
}

func extractGitDiff(ctx context.Context, instance sandbox.Instance, workdir string, baseline *WorkspaceBaseline, excludePaths []string) (string, error) {
	if baseline == nil || baseline.Revision == "" {
		return "", nil
	}
	opts := sandbox.ExecOptions{CWD: workdir}

	addResult, err := instance.Exec(ctx, sandbox.ShellCommand("git add -A"), opts)
	if err != nil {
		return "", nil
	}
	if addResult.ExitCode != 0 {
		return "", nil
	}

	for _, path := range excludePaths {
		instance.Exec(ctx, sandbox.ShellCommand(
			fmt.Sprintf("git reset HEAD -- %s 2>/dev/null", shellQuote(path)),
		), opts)
	}

	diffCmd := fmt.Sprintf("git diff --cached %s", baseline.Revision)
	diffResult, err := instance.Exec(ctx, sandbox.ShellCommand(diffCmd), opts)
	if err != nil {
		return "", nil
	}
	if diffResult.ExitCode != 0 {
		return "", nil
	}
	return diffResult.Stdout, nil
}

// excludeBaselineDirtiness returns paths that were untracked at baseline
// and still have matching content hashes -- these should be excluded from
// the agent's patch. Only reads sandbox state; never modifies files.
func excludeBaselineDirtiness(ctx context.Context, instance sandbox.Instance, workdir string, baseline *WorkspaceBaseline) []string {
	if baseline == nil || len(baseline.Untracked) == 0 {
		return nil
	}
	opts := sandbox.ExecOptions{CWD: workdir}
	var exclude []string
	for _, entry := range baseline.Untracked {
		if entry.ContentHash == "" {
			continue
		}
		hashResult, err := instance.Exec(ctx, sandbox.ShellCommand(
			fmt.Sprintf("sha256sum %s 2>/dev/null | cut -d' ' -f1", shellQuote(entry.Path)),
		), opts)
		if err != nil || hashResult.ExitCode != 0 {
			continue
		}
		currentHash := strings.TrimSpace(hashResult.Stdout)
		if currentHash == entry.ContentHash {
			exclude = append(exclude, entry.Path)
		}
	}
	return exclude
}

func ensureTrailingNewline(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

func countDiffHeaders(patch string) int {
	count := 0
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "diff -") {
			count++
		}
	}
	return count
}

func patchSHA256(patch string) string {
	h := sha256.Sum256([]byte(patch))
	return hex.EncodeToString(h[:])
}

func agentArtifactPolicy(agent domain.AgentSpec) *domain.ArtifactPolicySpec {
	if agent.Runtime == nil || agent.Runtime.Artifacts == nil {
		return nil
	}
	return agent.Runtime.Artifacts
}

func markAgentMissingRequiredPatch(result *domain.AgentResult, patchPath string) {
	result.Status = domain.AgentStatusFailed
	result.ExitReason = domain.AgentExitReasonCompletedNoPatch
	result.Summary = "agent completed without required patch"
	result.Error = fmt.Sprintf("required patch artifact %q was not produced", patchPath)
}

func readRemoteFileIfExists(ctx context.Context, instance sandbox.Instance, path string) (string, error) {
	command := fmt.Sprintf("if [ -f %s ]; then cat %s; fi", shellQuote(path), shellQuote(path))
	result, err := instance.Exec(ctx, sandbox.ShellCommand(command), sandbox.ExecOptions{})
	if err != nil {
		return "", fmt.Errorf("read agent patch %s: %w", path, err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read agent patch %s exited with status %d: %s", path, result.ExitCode, result.Stderr)
	}
	return result.Stdout, nil
}

func resolveAgentWorkdir(agent domain.AgentSpec) string {
	if agent.Runtime != nil && agent.Runtime.Workdir != "" {
		return agent.Runtime.Workdir
	}
	return "/workspace"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
