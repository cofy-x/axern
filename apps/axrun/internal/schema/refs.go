package schema

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func validateInputSpec(problems *collector, runDir string, path string, field string, input *domain.InputSpec) {
	if input == nil {
		return
	}
	if input.Path != "" {
		validateRunRef(problems, runDir, path, field+".path", input.Path, false)
		validateExistingRunRef(problems, runDir, path, field+".path", input.Path)
	}
}

func validateSourceRef(problems *collector, runDir string, path string, field string, source *domain.SourceRef) {
	if source == nil {
		return
	}
	if source.Path != "" {
		validateRunRef(problems, runDir, path, field+".path", source.Path, false)
		validateExistingRunRef(problems, runDir, path, field+".path", source.Path)
	}
}

func validateInitialStateSpec(problems *collector, runDir string, path string, field string, state *domain.InitialStateSpec) {
	if state == nil {
		return
	}
	if state.Path != "" {
		validateRunRef(problems, runDir, path, field+".path", state.Path, false)
		validateExistingRunRef(problems, runDir, path, field+".path", state.Path)
	}
	if state.Dockerfile != "" {
		validateRunRef(problems, runDir, path, field+".dockerfile", state.Dockerfile, false)
		validateExistingRunRef(problems, runDir, path, field+".dockerfile", state.Dockerfile)
	}
	for index, excludePath := range state.ExcludePaths {
		validateRelativeRef(problems, path, fmt.Sprintf("%s.exclude_paths[%d]", field, index), excludePath)
	}
}

func validateOracleSpec(problems *collector, runDir string, path string, oracle *domain.OracleSpec, taskSetTaskID string) {
	if oracle == nil || oracle.Path == "" {
		return
	}
	if taskSetTaskID != "" {
		validateTaskSetAssetPath(problems, path, "oracle.path", oracle.Path, pathpkg.Join("tasks", taskSetTaskID, "oracle"))
		return
	}
	validateRunRef(problems, runDir, path, "oracle.path", oracle.Path, false)
	validateExistingRunRef(problems, runDir, path, "oracle.path", oracle.Path)
}

func validateVerifierAssets(problems *collector, runDir string, path string, assets []domain.VerifierAssetSpec, taskSetTaskID string) {
	for index, asset := range assets {
		field := fmt.Sprintf("verifier.assets[%d]", index)
		if asset.Path == "" {
			problems.add(path, field+".path", "is required")
			continue
		}
		if taskSetTaskID != "" {
			validateTaskSetAssetPath(problems, path, field+".path", asset.Path, pathpkg.Join("tasks", taskSetTaskID, "verifier"))
		} else {
			validateRunRef(problems, runDir, path, field+".path", asset.Path, false)
			validateExistingRunRef(problems, runDir, path, field+".path", asset.Path)
		}
		if asset.TargetPath != "" && !filepath.IsAbs(asset.TargetPath) {
			problems.add(path, field+".target_path", "must be absolute")
		}
		if pathpkg.Clean(asset.TargetPath) == "/" {
			problems.add(path, field+".target_path", "must not be the sandbox root")
		}
	}
}

// validateTaskSetAssetPath validates a logical path inside a remote TaskSet
// payload. These assets intentionally do not exist in the local run directory;
// Axern materializes them from the resolved payload after the agent phase.
func validateTaskSetAssetPath(problems *collector, recordPath string, field string, value string, prefix string) {
	value = strings.TrimSpace(value)
	if value == "" {
		problems.add(recordPath, field, "is required")
		return
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, `\`) {
		problems.add(recordPath, field, "must be a relative TaskSet payload path")
		return
	}
	clean := pathpkg.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		problems.add(recordPath, field, "must not escape the TaskSet payload")
		return
	}
	if clean != prefix && !strings.HasPrefix(clean, prefix+"/") {
		problems.add(recordPath, field, fmt.Sprintf("must be inside TaskSet payload prefix %q", prefix))
	}
}

func validateSandboxRuntimeSourceRefs(problems *collector, runDir string, path string, source *domain.SandboxRuntimeSourceSpec) {
	if source == nil || source.Dockerfile == "" {
		return
	}
	validateRunRef(problems, runDir, path, "sandbox.runtime_source.dockerfile", source.Dockerfile, false)
	validateExistingRunRef(problems, runDir, path, "sandbox.runtime_source.dockerfile", source.Dockerfile)
}

func validateRelativeRef(problems *collector, path string, field string, ref string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return
	}
	if filepath.IsAbs(ref) {
		problems.add(path, field, "must be relative")
		return
	}
	clean := filepath.Clean(ref)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		problems.add(path, field, "must not escape the base directory")
	}
}

func validateRunRef(problems *collector, runDir string, path string, field string, ref string, required bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if required {
			problems.add(path, field, "is required")
		}
		return
	}
	if filepath.IsAbs(ref) {
		if sameCleanPath(ref, runDir) {
			return
		}
		problems.add(path, field, "must be run-root-relative")
		return
	}
	clean := filepath.Clean(ref)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		problems.add(path, field, "must not escape the run directory")
	}
}

func validateExistingRunRef(problems *collector, runDir string, path string, field string, ref string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return
	}
	target, ok := runRefPath(runDir, ref)
	if !ok {
		return
	}
	if _, err := os.Stat(target); err != nil {
		problems.add(path, field, fmt.Sprintf("referenced path does not exist: %v", err))
	}
}

func validateRunOutputPath(problems *collector, runDir string, path string, field string, ref string) {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "." {
		return
	}
	if filepath.IsAbs(ref) {
		if sameCleanPath(ref, runDir) {
			return
		}
		problems.add(path, field, "must point to the run directory")
		return
	}
	clean := filepath.Clean(ref)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		problems.add(path, field, "must not escape the run directory")
	}
}

func runRefPath(runDir string, ref string) (string, bool) {
	if filepath.IsAbs(ref) {
		if sameCleanPath(ref, runDir) {
			return ref, true
		}
		return "", false
	}
	clean := filepath.Clean(ref)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(runDir, filepath.FromSlash(ref)), true
}

func joinRunRef(runDir string, ref string, fallbackDir string, fallbackName string) string {
	if strings.TrimSpace(ref) == "" {
		return filepath.Join(fallbackDir, fallbackName)
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(runDir, filepath.FromSlash(ref))
}

func displayPath(runDir string, path string) string {
	rel, err := filepath.Rel(runDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(filepath.Clean(path))
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func sameCleanPath(a string, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	if evalA, err := filepath.EvalSymlinks(absA); err == nil {
		absA = evalA
	}
	if evalB, err := filepath.EvalSymlinks(absB); err == nil {
		absB = evalB
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}
