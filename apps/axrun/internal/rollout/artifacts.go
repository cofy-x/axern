package rollout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/runref"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func downloadConfiguredArtifacts(ctx context.Context, instance sandbox.Instance, agent domain.AgentSpec, artifactDir string, appendStep func(domain.TrajectoryStep) (int, error)) ([]domain.ArtifactRef, error) {
	if agent.Runtime == nil || agent.Runtime.Artifacts == nil || len(agent.Runtime.Artifacts.OutputPaths) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(artifactDir) == "" {
		return nil, fmt.Errorf("artifact directory is required")
	}
	var artifacts []domain.ArtifactRef
	for _, remotePath := range agent.Runtime.Artifacts.OutputPaths {
		remotePath = strings.TrimSpace(remotePath)
		if remotePath == "" {
			continue
		}
		resolvedRemotePath, err := resolveArtifactOutputPath(resolveAgentWorkdir(agent), remotePath)
		if err != nil {
			return nil, fmt.Errorf("resolve artifact %s: %w", remotePath, err)
		}
		localPath := filepath.Join(artifactDir, "downloads", artifactNameForRemotePath(remotePath))
		if err := instance.DownloadPath(ctx, resolvedRemotePath, localPath, sandbox.DownloadPathOptions{}); err != nil {
			return nil, fmt.Errorf("download artifact %s: %w", remotePath, err)
		}
		artifact := domain.ArtifactRef{
			Path:        runRelativeArtifactPath(artifactDir, localPath),
			Kind:        domain.ArtifactKindDownloadedDir,
			Description: fmt.Sprintf("downloaded artifact from %s", remotePath),
			Producer:    "rollout",
			Role:        domain.ArtifactRoleOutput,
		}
		if info, err := os.Stat(localPath); err == nil {
			if !info.IsDir() {
				artifact.Kind = domain.ArtifactKindDownloadedFile
				artifact.SizeBytes = info.Size()
				createdAt := info.ModTime().UTC()
				artifact.CreatedAt = &createdAt
				if artifact.MediaType == "" {
					artifact.MediaType = mime.TypeByExtension(filepath.Ext(localPath))
				}
			}
		}
		artifacts = appendArtifact(artifacts, artifact)
		if _, err := appendStep(domain.TrajectoryStep{
			Type:      domain.TrajectoryEventArtifactDownload,
			Actor:     "rollout",
			Summary:   fmt.Sprintf("downloaded artifact %s", remotePath),
			OutputRef: artifact.Path,
			Artifacts: []domain.ArtifactRef{artifact},
		}); err != nil {
			return artifacts, err
		}
	}
	return artifacts, nil
}

type taskOutputDownloadResult struct {
	Artifacts       []domain.ArtifactRef
	MissingRequired []string
}

func downloadTaskOutputs(ctx context.Context, instance sandbox.Instance, task domain.TaskInstance, artifactDir string, appendStep func(domain.TrajectoryStep) (int, error)) (taskOutputDownloadResult, error) {
	if len(task.Outputs) == 0 {
		return taskOutputDownloadResult{}, nil
	}
	exister, ok := instance.(sandbox.PathExister)
	if !ok {
		return taskOutputDownloadResult{}, fmt.Errorf("sandbox does not support TaskSet output existence checks")
	}
	var result taskOutputDownloadResult
	for _, output := range task.Outputs {
		remotePath, err := resolveArtifactOutputPath(task.Sandbox.Workdir, output.Path)
		if err != nil {
			return result, fmt.Errorf("resolve task output %q: %w", output.Path, err)
		}
		exists, err := exister.PathExists(ctx, remotePath)
		if err != nil {
			return result, fmt.Errorf("check task output %q: %w", output.Path, err)
		}
		if !exists {
			if output.Required {
				result.MissingRequired = append(result.MissingRequired, output.Path)
			}
			continue
		}
		localPath := filepath.Join(artifactDir, "outputs", artifactNameForRemotePath(output.Path))
		if err := instance.DownloadPath(ctx, remotePath, localPath, sandbox.DownloadPathOptions{}); err != nil {
			return result, fmt.Errorf("download task output %q: %w", output.Path, err)
		}
		artifact := domain.ArtifactRef{
			Path:        runRelativeArtifactPath(artifactDir, localPath),
			Kind:        domain.ArtifactKindDownloadedDir,
			Description: fmt.Sprintf("TaskSet output from %s", output.Path),
			Producer:    "rollout",
			Role:        domain.ArtifactRoleOutput,
		}
		if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
			artifact.Kind = domain.ArtifactKindDownloadedFile
			artifact.SizeBytes = info.Size()
			createdAt := info.ModTime().UTC()
			artifact.CreatedAt = &createdAt
			artifact.MediaType = mime.TypeByExtension(filepath.Ext(localPath))
		}
		result.Artifacts = appendArtifact(result.Artifacts, artifact)
		if _, err := appendStep(domain.TrajectoryStep{
			Type:      domain.TrajectoryEventArtifactDownload,
			Actor:     "rollout",
			Summary:   fmt.Sprintf("downloaded TaskSet output %s", output.Path),
			OutputRef: artifact.Path,
			Artifacts: []domain.ArtifactRef{artifact},
		}); err != nil {
			return result, err
		}
	}
	return result, nil
}

func resolveArtifactOutputPath(workdir string, outputPath string) (string, error) {
	outputPath = strings.TrimSpace(outputPath)
	if err := contract.ValidateArtifactOutputPath(outputPath); err != nil {
		return "", err
	}
	clean := path.Clean(outputPath)
	if path.IsAbs(clean) {
		return clean, nil
	}
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		workdir = "/workspace"
	}
	if !path.IsAbs(workdir) {
		return "", fmt.Errorf("agent workdir %q must be absolute", workdir)
	}
	return path.Join(path.Clean(workdir), clean), nil
}

func captureAgentArtifacts(store Store, paths Paths, result *domain.AgentResult) error {
	if result == nil || paths.ArtifactDir == "" {
		return nil
	}
	hadRawLog := result.RawLogRef != ""
	capturedStdoutRef := ""
	capturedStderrRef := ""
	if result.Stdout != "" && result.StdoutRef == "" {
		path, err := store.WriteAgentArtifact(paths.ArtifactDir, "agent.stdout.txt", result.Stdout)
		if err != nil {
			return err
		}
		result.StdoutRef = path
		capturedStdoutRef = path
		result.Artifacts = appendArtifact(result.Artifacts, artifactForPath(path, domain.ArtifactKindAgentStdout, "agent stdout"))
	}
	if result.Stderr != "" && result.StderrRef == "" {
		path, err := store.WriteAgentArtifact(paths.ArtifactDir, "agent.stderr.txt", result.Stderr)
		if err != nil {
			return err
		}
		result.StderrRef = path
		capturedStderrRef = path
		result.Artifacts = appendArtifact(result.Artifacts, artifactForPath(path, domain.ArtifactKindAgentStderr, "agent stderr"))
	}
	if result.RawLogRef == "" && (result.StdoutRef != "" || result.StderrRef != "") {
		timestamp := time.Now().UTC()
		event := domain.AgentRawEvent{
			Type:      domain.AgentRawEventCommandFinished,
			Timestamp: &timestamp,
			ExitCode:  result.ExitCode,
			StdoutRef: result.StdoutRef,
			StderrRef: result.StderrRef,
		}
		path, err := store.AppendAgentRawEvent(paths.ArtifactDir, event)
		if err != nil {
			return err
		}
		result.RawLogRef = path
		result.Artifacts = appendArtifact(result.Artifacts, artifactForPath(path, domain.ArtifactKindAgentRawLog, "agent raw log"))
	}
	if hadRawLog {
		if err := appendCapturedAgentArtifactEvents(store, paths.ArtifactDir, capturedStdoutRef, capturedStderrRef); err != nil {
			return err
		}
	}
	result.Artifacts = appendAgentResultArtifactRefs(result.Artifacts, *result)
	enrichArtifactDigests(paths.ArtifactDir, result.Artifacts)
	return nil
}

func writeArtifactManifest(store Store, paths Paths, episode domain.Episode, nowFn func() time.Time) (domain.Episode, error) {
	if strings.TrimSpace(paths.ArtifactDir) == "" {
		return episode, nil
	}
	manifest := domain.ArtifactManifest{
		SchemaVersion: domain.LocalSchemaVersion,
		EpisodeID:     episode.ID,
		GeneratedAt:   now(nowFn),
		Entries:       artifactManifestEntries(paths.ArtifactDir, episode.Artifacts),
	}
	path, err := store.WriteArtifactManifest(paths.ArtifactDir, manifest)
	if err != nil {
		return episode, err
	}
	episode.ArtifactManifestPath = path
	return episode, nil
}

func artifactManifestEntries(artifactDir string, artifacts []domain.ArtifactRef) []domain.ArtifactManifestEntry {
	entries := make([]domain.ArtifactManifestEntry, 0, len(artifacts))
	seen := map[string]struct{}{}
	runDir := runref.RunDirFromArtifactDir(artifactDir)
	for _, artifact := range artifacts {
		path := strings.TrimSpace(artifact.Path)
		if path == "" {
			continue
		}
		cleanPath := filepath.ToSlash(filepath.Clean(path))
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}
		entry := domain.ArtifactManifestEntry{
			Kind:        artifact.Kind,
			Source:      artifact.Path,
			Path:        cleanPath,
			Status:      domain.ArtifactManifestStatusPresent,
			SHA256:      artifact.SHA256,
			MediaType:   artifact.MediaType,
			SizeBytes:   artifact.SizeBytes,
			Description: artifact.Description,
			Producer:    artifact.Producer,
			Role:        artifact.Role,
		}
		if filepath.IsAbs(cleanPath) {
			entry.Status = domain.ArtifactManifestStatusFailed
			entry.Error = "artifact path must be run-root-relative"
			entries = append(entries, entry)
			continue
		}
		absPath := filepath.Join(runDir, filepath.FromSlash(cleanPath))
		info, err := os.Stat(absPath)
		switch {
		case err == nil:
			if info.IsDir() {
				entry.SizeBytes = 0
			} else {
				if entry.SHA256 == "" || entry.SizeBytes == 0 {
					sha, size, digestErr := computeFileDigest(absPath)
					if digestErr == nil {
						if entry.SHA256 == "" {
							entry.SHA256 = sha
						}
						if entry.SizeBytes == 0 {
							entry.SizeBytes = size
						}
					}
				}
				if entry.MediaType == "" {
					entry.MediaType = mediaTypeForArtifact(entry.Kind, cleanPath)
				}
			}
		case os.IsNotExist(err):
			entry.Status = domain.ArtifactManifestStatusMissing
			entry.Error = err.Error()
		default:
			entry.Status = domain.ArtifactManifestStatusFailed
			entry.Error = err.Error()
		}
		entries = append(entries, entry)
	}
	return entries
}

func appendCapturedAgentArtifactEvents(store Store, artifactDir string, stdoutRef string, stderrRef string) error {
	if stdoutRef != "" {
		timestamp := time.Now().UTC()
		if _, err := store.AppendAgentRawEvent(artifactDir, domain.AgentRawEvent{
			Timestamp:    &timestamp,
			Type:         domain.AgentRawEventArtifact,
			ArtifactRef:  stdoutRef,
			ArtifactKind: domain.ArtifactKindAgentStdout,
			StdoutRef:    stdoutRef,
		}); err != nil {
			return err
		}
	}
	if stderrRef != "" {
		timestamp := time.Now().UTC()
		if _, err := store.AppendAgentRawEvent(artifactDir, domain.AgentRawEvent{
			Timestamp:    &timestamp,
			Type:         domain.AgentRawEventArtifact,
			ArtifactRef:  stderrRef,
			ArtifactKind: domain.ArtifactKindAgentStderr,
			StderrRef:    stderrRef,
		}); err != nil {
			return err
		}
	}
	return nil
}

func appendAgentResultArtifactRefs(artifacts []domain.ArtifactRef, result domain.AgentResult) []domain.ArtifactRef {
	if result.StdoutRef != "" {
		artifacts = appendArtifact(artifacts, artifactForPath(result.StdoutRef, domain.ArtifactKindAgentStdout, "agent stdout"))
	}
	if result.StderrRef != "" {
		artifacts = appendArtifact(artifacts, artifactForPath(result.StderrRef, domain.ArtifactKindAgentStderr, "agent stderr"))
	}
	if result.RawLogRef != "" {
		artifacts = appendArtifact(artifacts, artifactForPath(result.RawLogRef, domain.ArtifactKindAgentRawLog, "agent raw log"))
	}
	if result.PatchRef != "" {
		artifacts = appendArtifact(artifacts, artifactForPath(result.PatchRef, domain.ArtifactKindPatch, "agent patch"))
	}
	return artifacts
}

func artifactForPath(path string, kind domain.ArtifactKind, description string) domain.ArtifactRef {
	return domain.ArtifactRef{
		Path:        path,
		Kind:        kind,
		Description: description,
		Producer:    "rollout",
		Role:        artifactRole(kind),
		MediaType:   mediaTypeForArtifact(kind, path),
	}
}

func artifactRole(kind domain.ArtifactKind) domain.ArtifactRole {
	switch kind {
	case domain.ArtifactKindAgentRawLog:
		return domain.ArtifactRoleRaw
	case domain.ArtifactKindAgentStdout, domain.ArtifactKindAgentStderr, domain.ArtifactKindDownloadedFile, domain.ArtifactKindDownloadedDir, domain.ArtifactKindPatch:
		return domain.ArtifactRoleOutput
	case domain.ArtifactKindVerifierBreakdown:
		return domain.ArtifactRoleDerived
	default:
		return ""
	}
}

func appendArtifact(artifacts []domain.ArtifactRef, artifact domain.ArtifactRef) []domain.ArtifactRef {
	if artifact.Path == "" {
		return artifacts
	}
	cleanPath := filepath.Clean(artifact.Path)
	for _, existing := range artifacts {
		if filepath.Clean(existing.Path) == cleanPath {
			return artifacts
		}
	}
	return append(artifacts, artifact)
}

func artifactRefsForKind(artifacts []domain.ArtifactRef, kinds ...domain.ArtifactKind) []domain.ArtifactRef {
	if len(artifacts) == 0 || len(kinds) == 0 {
		return nil
	}
	allowed := map[domain.ArtifactKind]struct{}{}
	for _, kind := range kinds {
		allowed[kind] = struct{}{}
	}
	var refs []domain.ArtifactRef
	for _, artifact := range artifacts {
		if _, ok := allowed[artifact.Kind]; ok {
			refs = appendArtifact(refs, artifact)
		}
	}
	return refs
}

func artifactNameForRemotePath(remotePath string) string {
	name := strings.TrimSpace(filepath.Clean(remotePath))
	name = strings.TrimPrefix(name, string(filepath.Separator))
	name = strings.TrimPrefix(name, "/")
	if name == "." || name == "" {
		return "root"
	}
	replacer := strings.NewReplacer("/", "_", `\`, "_", ":", "_")
	return replacer.Replace(name)
}

func runRelativeArtifactPath(artifactDir string, path string) string {
	return runref.ArtifactPath(artifactDir, path)
}

var digestKinds = map[domain.ArtifactKind]struct{}{
	domain.ArtifactKindAgentRawLog: {},
	domain.ArtifactKindPatch:       {},
	domain.ArtifactKindAgentStdout: {},
	domain.ArtifactKindAgentStderr: {},
}

// enrichArtifactDigests populates SHA256 and SizeBytes on key artifact
// refs by reading the files from disk. Non-existent files or errors
// are silently skipped — digest enrichment is best-effort.
func enrichArtifactDigests(artifactDir string, artifacts []domain.ArtifactRef) {
	if artifactDir == "" {
		return
	}
	runDir := runref.RunDirFromArtifactDir(artifactDir)
	for i := range artifacts {
		if artifacts[i].MediaType == "" {
			artifacts[i].MediaType = mediaTypeForArtifact(artifacts[i].Kind, artifacts[i].Path)
		}
		if _, ok := digestKinds[artifacts[i].Kind]; !ok {
			continue
		}
		if artifacts[i].SHA256 != "" {
			continue
		}
		absPath := filepath.Join(runDir, filepath.FromSlash(artifacts[i].Path))
		sha, size, err := computeFileDigest(absPath)
		if err != nil || sha == "" {
			continue
		}
		artifacts[i].SHA256 = sha
		artifacts[i].SizeBytes = size
	}
}

func mediaTypeForArtifact(kind domain.ArtifactKind, path string) string {
	switch kind {
	case domain.ArtifactKindAgentRawLog:
		return "application/x-ndjson"
	case domain.ArtifactKindPatch:
		return "text/x-diff"
	case domain.ArtifactKindAgentStdout, domain.ArtifactKindAgentStderr:
		return "text/plain"
	case domain.ArtifactKindVerifierBreakdown:
		return "application/json"
	}
	return mime.TypeByExtension(filepath.Ext(path))
}

func computeFileDigest(path string) (sha256Hex string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, nil
		}
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
