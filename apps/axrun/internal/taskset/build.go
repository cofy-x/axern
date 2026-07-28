package taskset

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	axernsdk "github.com/cofy-x/axern/sdk/go"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"gopkg.in/yaml.v3"
)

type BuildParams struct {
	File   string
	Output string
}

type BuildResult struct {
	Output       string `json:"output"`
	SourceDigest string `json:"source_digest"`
	TaskCount    int    `json:"task_count"`
}

type expandedTask struct {
	id          string
	instruction string
	sources     []string
	generator   Generator
}

func Build(params BuildParams) (BuildResult, error) {
	file, err := filepath.Abs(filepath.Clean(strings.TrimSpace(params.File)))
	if err != nil || strings.TrimSpace(params.File) == "" {
		return BuildResult{}, fmt.Errorf("task set build file is required")
	}
	outputValue := strings.TrimSpace(params.Output)
	if outputValue == "" || filepath.Clean(outputValue) == "." {
		return BuildResult{}, fmt.Errorf("task set build output is required")
	}
	output, err := filepath.Abs(filepath.Clean(outputValue))
	if err != nil {
		return BuildResult{}, fmt.Errorf("resolve task set build output: %w", err)
	}
	if _, err := os.Stat(output); err == nil {
		return BuildResult{}, fmt.Errorf("task set build output %q already exists", output)
	} else if !os.IsNotExist(err) {
		return BuildResult{}, fmt.Errorf("stat task set build output: %w", err)
	}
	envelope, err := loadBuild(file)
	if err != nil {
		return BuildResult{}, err
	}
	tasks, err := expandBuild(envelope, filepath.Dir(file))
	if err != nil {
		return BuildResult{}, err
	}
	// Compile outside the build root. An adjacent staging directory would become
	// part of a generator whose workspace is "." and recursively contaminate the
	// payload being built.
	stage, err := os.MkdirTemp("", "axrun-taskset-build-*")
	if err != nil {
		return BuildResult{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	payloadRoot := filepath.Join(stage, "payload")
	descriptor := Descriptor{
		APIVersion: APIVersion,
		Kind:       DescriptorKind,
		Name:       envelope.Metadata.Name,
		Provenance: Provenance{
			Compiler:      "axrun",
			Contract:      APIVersion,
			PackerVersion: PackerVersion,
		},
	}
	for _, task := range tasks {
		compiled, err := materializeTask(payloadRoot, filepath.Dir(file), task)
		if err != nil {
			return BuildResult{}, err
		}
		descriptor.Tasks = append(descriptor.Tasks, compiled)
	}
	payload, err := deterministicTar(payloadRoot)
	if err != nil {
		return BuildResult{}, err
	}
	payloadDigest := sha256.Sum256(payload)
	descriptor.SourceDigest, err = computeSourceDigest(descriptor, payloadDigest)
	if err != nil {
		return BuildResult{}, err
	}
	if err := os.WriteFile(filepath.Join(stage, "payload.tar"), payload, 0o644); err != nil {
		return BuildResult{}, err
	}
	if err := writePayloadOCILayout(stage); err != nil {
		return BuildResult{}, err
	}
	if err := writeJSON(filepath.Join(stage, "descriptor.json"), descriptor); err != nil {
		return BuildResult{}, err
	}
	receipt := BuildReceipt{
		SchemaVersion:  APIVersion,
		SourceDigest:   descriptor.SourceDigest,
		DescriptorPath: "descriptor.json",
		PayloadPath:    "payload.tar",
		OCILayoutPath:  "oci",
		TaskCount:      len(descriptor.Tasks),
	}
	if err := writeJSON(filepath.Join(stage, "build.json"), receipt); err != nil {
		return BuildResult{}, err
	}
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return BuildResult{}, err
	}
	commitStage, err := os.MkdirTemp(parent, ".axrun-taskset-commit-*")
	if err != nil {
		return BuildResult{}, err
	}
	defer func() { _ = os.RemoveAll(commitStage) }()
	if err := copyExact(stage, commitStage, nil); err != nil {
		return BuildResult{}, fmt.Errorf("stage task set build: %w", err)
	}
	if err := os.Rename(commitStage, output); err != nil {
		return BuildResult{}, fmt.Errorf("commit task set build: %w", err)
	}
	return BuildResult{Output: output, SourceDigest: descriptor.SourceDigest, TaskCount: len(descriptor.Tasks)}, nil
}

func computeSourceDigest(descriptor Descriptor, payloadDigest [sha256.Size]byte) (string, error) {
	descriptor.SourceDigest = ""
	canonicalTasks, err := json.Marshal(descriptor)
	if err != nil {
		return "", err
	}
	sourceHash := sha256.New()
	_, _ = sourceHash.Write([]byte("axrun-taskset-source-v1\x00"))
	_, _ = sourceHash.Write(canonicalTasks)
	_, _ = sourceHash.Write([]byte{0})
	_, _ = sourceHash.Write(payloadDigest[:])
	return "sha256:" + hex.EncodeToString(sourceHash.Sum(nil)), nil
}

func writePayloadOCILayout(stage string) error {
	layer, err := tarball.LayerFromFile(filepath.Join(stage, "payload.tar"))
	if err != nil {
		return fmt.Errorf("create payload OCI layer: %w", err)
	}
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return fmt.Errorf("create payload OCI image: %w", err)
	}
	path, err := layout.Write(filepath.Join(stage, "oci"), empty.Index)
	if err != nil {
		return fmt.Errorf("create payload OCI layout: %w", err)
	}
	if err := path.AppendImage(image, layout.WithAnnotations(map[string]string{"org.opencontainers.image.ref.name": "taskset-payload"})); err != nil {
		return fmt.Errorf("write payload OCI layout: %w", err)
	}
	return nil
}

func loadBuild(path string) (BuildEnvelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BuildEnvelope{}, fmt.Errorf("read task set build: %w", err)
	}
	var envelope BuildEnvelope
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&envelope); err != nil {
		return BuildEnvelope{}, fmt.Errorf("parse task set build: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return BuildEnvelope{}, fmt.Errorf("task set build must contain one document")
	}
	if envelope.APIVersion != APIVersion || envelope.Kind != BuildKind {
		return BuildEnvelope{}, fmt.Errorf("task set build requires api_version %q and kind %s", APIVersion, BuildKind)
	}
	if strings.TrimSpace(envelope.Metadata.Name) == "" {
		return BuildEnvelope{}, fmt.Errorf("metadata.name is required")
	}
	if len(envelope.Spec.Generators) == 0 {
		return BuildEnvelope{}, fmt.Errorf("spec.generators must not be empty")
	}
	return envelope, nil
}

func expandBuild(envelope BuildEnvelope, root string) ([]expandedTask, error) {
	var out []expandedTask
	seen := map[string]bool{}
	for index, generator := range envelope.Spec.Generators {
		if err := validateExcludePaths(generator.ExcludePaths); err != nil {
			return nil, fmt.Errorf("generator %d exclude_paths: %w", index+1, err)
		}
		if err := validateTaskTemplate(generator.Task); err != nil {
			return nil, fmt.Errorf("generator %d task: %w", index+1, err)
		}
		instruction, err := resolveInstruction(root, generator.Instruction)
		if err != nil {
			return nil, fmt.Errorf("generator %d instruction: %w", index+1, err)
		}
		matches, err := expandPaths(root, generator.Workspace.Paths)
		if err != nil {
			return nil, fmt.Errorf("generator %d workspace: %w", index+1, err)
		}
		switch generator.Workspace.Expand {
		case "aggregate":
			id := strings.TrimSpace(generator.TaskID)
			if err := contract.ValidatePathSegment("task id", id); err != nil {
				return nil, fmt.Errorf("generator %d: %w", index+1, err)
			}
			out = append(out, expandedTask{id: id, instruction: instruction, sources: matches, generator: generator})
		case "per_match":
			prefix := strings.Trim(strings.TrimSpace(generator.TaskIDPrefix), "-")
			if err := validateTaskIDPrefix(prefix); err != nil {
				return nil, fmt.Errorf("generator %d: %w", index+1, err)
			}
			for _, match := range matches {
				id, err := generatedTaskID(prefix, root, match, generator.ExcludePaths)
				if err != nil {
					return nil, fmt.Errorf("generator %d workspace: %w", index+1, err)
				}
				if err := contract.ValidatePathSegment("generated task id", id); err != nil {
					return nil, fmt.Errorf("generator %d workspace: %w", index+1, err)
				}
				out = append(out, expandedTask{id: id, instruction: instruction, sources: []string{match}, generator: generator})
			}
		default:
			return nil, fmt.Errorf("generator %d workspace.expand must be aggregate or per_match", index+1)
		}
	}
	for _, task := range out {
		if seen[task.id] {
			return nil, fmt.Errorf("task id %q is duplicated", task.id)
		}
		seen[task.id] = true
	}
	return out, nil
}

func validateTaskIDPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if len(prefix) > 40 {
		return fmt.Errorf("task_id_prefix must not exceed 40 bytes")
	}
	for index, r := range prefix {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' && index > 0 {
			continue
		}
		return fmt.Errorf("task_id_prefix %q must contain only lowercase ASCII letters, digits, and internal hyphens", prefix)
	}
	if strings.HasSuffix(prefix, "-") {
		return fmt.Errorf("task_id_prefix %q must not end with a hyphen", prefix)
	}
	return nil
}

func validateTaskTemplate(task TaskTemplate) error {
	if task.Sandbox.Resources != nil {
		return fmt.Errorf("sandbox.resources is not part of TaskSet; use task.resources")
	}
	if task.Sandbox.Workdir == "" || !strings.HasPrefix(task.Sandbox.Workdir, "/") {
		return fmt.Errorf("sandbox.workdir must be absolute")
	}
	if hasParentPathComponent(task.Sandbox.Workdir) {
		return fmt.Errorf("sandbox.workdir must not contain parent references")
	}
	workdir := filepath.ToSlash(filepath.Clean(filepath.FromSlash(task.Sandbox.Workdir)))
	if workdir == "/" {
		return fmt.Errorf("sandbox.workdir must not be the sandbox root")
	}
	for _, asset := range task.Verifier.Assets {
		if strings.TrimSpace(asset.TargetPath) == "" {
			continue
		}
		if hasParentPathComponent(asset.TargetPath) {
			return fmt.Errorf("verifier asset target_path %q must not contain parent references", asset.TargetPath)
		}
		target := filepath.ToSlash(filepath.Clean(filepath.FromSlash(asset.TargetPath)))
		if target != workdir && !strings.HasPrefix(target, workdir+"/") {
			return fmt.Errorf("verifier asset target_path %q must be inside sandbox.workdir %q", asset.TargetPath, task.Sandbox.Workdir)
		}
	}
	if err := validateTaskOutputs(task.Outputs); err != nil {
		return err
	}
	if err := contract.ValidateVerifierSpec("task", task.Verifier); err != nil {
		return err
	}
	if err := contract.ValidateTimeoutPolicy("task", task.Timeouts); err != nil {
		return err
	}
	if err := validateTaskResources(task.Resources); err != nil {
		return err
	}
	if task.Sandbox.Backend != domain.SandboxBackendAxern {
		return nil
	}
	if task.Sandbox.RuntimeSource == nil {
		return fmt.Errorf("sandbox.runtime_source is required for axern")
	}
	source := task.Sandbox.RuntimeSource
	switch source.Type {
	case domain.SandboxRuntimeSourceImage:
		if strings.TrimSpace(source.Image) == "" {
			return fmt.Errorf("sandbox.runtime_source.image is required")
		}
		ref, err := name.NewDigest(strings.TrimSpace(source.Image), name.WeakValidation)
		if err != nil || !strings.HasPrefix(ref.DigestStr(), "sha256:") || len(strings.TrimPrefix(ref.DigestStr(), "sha256:")) != 64 {
			return fmt.Errorf("sandbox.runtime_source.image must use an immutable sha256 digest reference")
		}
	case domain.SandboxRuntimeSourceTemplate:
		if strings.TrimSpace(source.TemplateID) == "" {
			return fmt.Errorf("sandbox.runtime_source.template_id is required")
		}
	case domain.SandboxRuntimeSourceDockerfile:
		return fmt.Errorf("sandbox.runtime_source.type dockerfile is not supported by TaskSet; publish an immutable runtime image first")
	default:
		return fmt.Errorf("sandbox.runtime_source.type is invalid")
	}
	return nil
}

func validateTaskResources(resources *domain.ResourceSpec) error {
	if resources == nil {
		return nil
	}
	if strings.TrimSpace(resources.Disk) != "" {
		return fmt.Errorf("task.resources.disk is not supported until Axern exposes an ephemeral workspace disk contract")
	}
	_, err := axernsdk.NewSandbox(axernsdk.SandboxOptions{
		Client:        &axernsdk.Client{},
		TemplateID:    "axrun-taskset-resource-validation",
		RequestCPU:    axernsdk.ResourceQuantity(resources.RequestCPU),
		RequestMemory: axernsdk.ResourceQuantity(resources.RequestMemory),
		LimitCPU:      axernsdk.ResourceQuantity(resources.LimitCPU),
		LimitMemory:   axernsdk.ResourceQuantity(resources.LimitMemory),
	})
	if err != nil {
		return fmt.Errorf("task.resources: %w", err)
	}
	return nil
}

func validateTaskOutputs(outputs []domain.TaskOutputSpec) error {
	seenOutputs := map[string]bool{}
	for _, output := range outputs {
		value := strings.TrimSpace(output.Path)
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
		if value == "" || filepath.IsAbs(filepath.FromSlash(value)) || clean == ".." || strings.HasPrefix(clean, "../") || hasParentPathComponent(value) {
			return fmt.Errorf("output path %q must be workspace-relative and must not escape", output.Path)
		}
		if seenOutputs[clean] {
			return fmt.Errorf("output path %q is duplicated", output.Path)
		}
		seenOutputs[clean] = true
		if strings.TrimSpace(output.JSONSchema) != "" && !json.Valid([]byte(output.JSONSchema)) {
			return fmt.Errorf("output %q json_schema must be valid JSON", output.Path)
		}
	}
	return nil
}

func hasParentPathComponent(value string) bool {
	for _, component := range strings.Split(filepath.ToSlash(value), "/") {
		if component == ".." {
			return true
		}
	}
	return false
}

func resolveInstruction(root string, instruction Instruction) (string, error) {
	text, ref := strings.TrimSpace(instruction.Text), strings.TrimSpace(instruction.Path)
	if (text == "") == (ref == "") {
		return "", fmt.Errorf("exactly one of text or path is required")
	}
	if text != "" {
		return text, nil
	}
	path, err := resolveBuildPath(root, ref)
	if err != nil {
		return "", err
	}
	if err := rejectLinkedPath(root, path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || fileHasMultipleLinks(info) {
		return "", fmt.Errorf("instruction path %q must be a regular non-linked file", ref)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(string(data))
	if text == "" {
		return "", fmt.Errorf("instruction path is empty")
	}
	return text, nil
}

func expandPaths(root string, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, fmt.Errorf("paths must not be empty")
	}
	unique := map[string]bool{}
	var out []string
	for _, pattern := range patterns {
		resolved, err := resolveBuildPath(root, pattern)
		if err != nil {
			return nil, err
		}
		matches, err := filepath.Glob(resolved)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("path pattern %q matched nothing", pattern)
		}
		for _, match := range matches {
			match = filepath.Clean(match)
			if err := rejectLinkedPath(root, match); err != nil {
				return nil, fmt.Errorf("path pattern %q: %w", pattern, err)
			}
			if !unique[match] {
				unique[match] = true
				out = append(out, match)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func rejectLinkedPath(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes the build root", target)
	}
	current := filepath.Clean(root)
	components := []string{"."}
	if rel != "." {
		components = strings.Split(rel, string(filepath.Separator))
	}
	for _, component := range components {
		if component != "." {
			current = filepath.Join(current, component)
		}
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed at %s", current)
		}
	}
	return nil
}

func validateExcludePaths(patterns []string) error {
	for _, raw := range patterns {
		pattern := strings.TrimSpace(raw)
		if pattern == "" || filepath.IsAbs(filepath.FromSlash(pattern)) {
			return fmt.Errorf("pattern %q must be a non-empty relative path", raw)
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(pattern)))
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("pattern %q escapes the workspace", raw)
		}
		if _, err := filepath.Match(filepath.FromSlash(pattern), "validation"); err != nil {
			return fmt.Errorf("pattern %q is invalid: %w", raw, err)
		}
	}
	return nil
}

func resolveBuildPath(root, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" || filepath.IsAbs(ref) {
		return "", fmt.Errorf("path %q must be build-file-relative", ref)
	}
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(ref)))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the build root", ref)
	}
	return path, nil
}

func generatedTaskID(prefix, root, source string, excludes []string) (string, error) {
	rel, _ := filepath.Rel(root, source)
	base := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "task"
	}
	digest, err := sourceContentDigest(source, excludes)
	if err != nil {
		return "", err
	}
	hashSuffix := hex.EncodeToString(digest[:4])
	reserved := len(hashSuffix) + 1
	if prefix != "" {
		reserved += len(prefix) + 1
	}
	maxSlug := contract.MaxPathSegmentBytes - reserved
	if maxSlug < 1 {
		return "", fmt.Errorf("task_id_prefix leaves no room for a generated task id")
	}
	if len(slug) > maxSlug {
		slug = strings.Trim(slug[:maxSlug], "-")
		if slug == "" {
			slug = "task"
		}
	}
	parts := []string{prefix, slug, hashSuffix}
	var clean []string
	for _, part := range parts {
		if part != "" {
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, "-"), nil
}

func sourceContentDigest(source string, excludes []string) ([sha256.Size]byte, error) {
	var paths []string
	if err := filepath.WalkDir(source, func(current string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		paths = append(paths, current)
		return nil
	}); err != nil {
		return [sha256.Size]byte{}, err
	}
	sort.Slice(paths, func(i, j int) bool {
		a, _ := filepath.Rel(source, paths[i])
		b, _ := filepath.Rel(source, paths[j])
		return filepath.ToSlash(a) < filepath.ToSlash(b)
	})
	hash := sha256.New()
	for _, current := range paths {
		info, err := os.Lstat(current)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		rel, err := filepath.Rel(source, current)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		if excluded(filepath.ToSlash(rel), excludes) {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) || info.Mode().IsRegular() && fileHasMultipleLinks(info) {
			return [sha256.Size]byte{}, fmt.Errorf("unsupported file type or link at %s", current)
		}
		_, _ = io.WriteString(hash, filepath.ToSlash(rel))
		_, _ = hash.Write([]byte{0, byte(info.Mode().Perm() & 0o111), 0})
		if info.Mode().IsRegular() {
			file, err := os.Open(current)
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			_, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return [sha256.Size]byte{}, copyErr
			}
			if closeErr != nil {
				return [sha256.Size]byte{}, closeErr
			}
		}
		_, _ = hash.Write([]byte{0})
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func materializeTask(payloadRoot, buildRoot string, task expandedTask) (Task, error) {
	taskRoot := filepath.Join(payloadRoot, "tasks", task.id)
	workspace := filepath.Join(taskRoot, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return Task{}, err
	}
	for _, source := range task.sources {
		if err := copySource(source, workspace, task.generator.ExcludePaths); err != nil {
			return Task{}, fmt.Errorf("task %q workspace: %w", task.id, err)
		}
	}
	verifier := task.generator.Task.Verifier
	for index, asset := range verifier.Assets {
		source, err := resolveBuildPath(buildRoot, asset.Path)
		if err != nil {
			return Task{}, fmt.Errorf("task %q verifier asset: %w", task.id, err)
		}
		if err := rejectLinkedPath(buildRoot, source); err != nil {
			return Task{}, fmt.Errorf("task %q verifier asset: %w", task.id, err)
		}
		target := filepath.Join(taskRoot, "verifier", fmt.Sprintf("%03d-%s", index+1, filepath.Base(source)))
		if err := copyExact(source, target, nil); err != nil {
			return Task{}, err
		}
		verifier.Assets[index].Path = filepath.ToSlash(filepath.Join("tasks", task.id, "verifier", filepath.Base(target)))
	}
	oracle := task.generator.Task.Oracle
	oracleSubpath := ""
	if oracle != nil && strings.TrimSpace(oracle.Path) != "" {
		source, err := resolveBuildPath(buildRoot, oracle.Path)
		if err != nil {
			return Task{}, err
		}
		if err := rejectLinkedPath(buildRoot, source); err != nil {
			return Task{}, fmt.Errorf("task %q oracle asset: %w", task.id, err)
		}
		target := filepath.Join(taskRoot, "oracle", filepath.Base(source))
		if err := copyExact(source, target, nil); err != nil {
			return Task{}, err
		}
		copy := *oracle
		copy.Path = filepath.ToSlash(filepath.Join("tasks", task.id, "oracle", filepath.Base(source)))
		oracle = &copy
		oracleSubpath = filepath.ToSlash(filepath.Join("tasks", task.id, "oracle"))
	}
	instance := domain.TaskInstance{
		ID:           task.id,
		Instruction:  task.instruction,
		Sandbox:      task.generator.Task.Sandbox,
		Verifier:     verifier,
		Timeouts:     task.generator.Task.Timeouts,
		Resources:    task.generator.Task.Resources,
		Oracle:       oracle,
		Tags:         append([]string(nil), task.generator.Task.Tags...),
		Capabilities: append([]string(nil), task.generator.Task.Capabilities...),
		Outputs:      append([]domain.TaskOutputSpec(nil), task.generator.Task.Outputs...),
	}
	instance.InitialState = &domain.InitialStateSpec{
		Type:    "taskset_workspace",
		Path:    filepath.ToSlash(filepath.Join("tasks", task.id, "workspace")),
		Workdir: instance.Sandbox.Workdir,
	}
	return Task{
		Instance:         instance,
		WorkspaceSubpath: instance.InitialState.Path,
		VerifierSubpath:  filepath.ToSlash(filepath.Join("tasks", task.id, "verifier")),
		OracleSubpath:    oracleSubpath,
	}, nil
}

func copySource(source, workspace string, excludes []string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyExact(source, workspace, excludes)
	}
	return copyExact(source, filepath.Join(workspace, filepath.Base(source)), excludes)
}

func copyExact(source, target string, excludes []string) error {
	root := source
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() || info.Mode().IsRegular() && fileHasMultipleLinks(info) {
			return fmt.Errorf("unsupported file type at %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if excluded(filepath.ToSlash(rel), excludes) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := target
		if rel != "." {
			destination = filepath.Join(target, rel)
		}
		if info.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inCloseErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inCloseErr != nil {
			return inCloseErr
		}
		return closeErr
	})
}

func excluded(rel string, patterns []string) bool {
	if rel == "." {
		return false
	}
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(rel)); matched {
			return true
		}
	}
	return false
}

func deterministicTar(root string) ([]byte, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(paths, func(i, j int) bool {
		a, _ := filepath.Rel(root, paths[i])
		b, _ := filepath.Rel(root, paths[j])
		return filepath.ToSlash(a) < filepath.ToSlash(b)
	})
	var buffer bytes.Buffer
	tw := tar.NewWriter(&buffer)
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(root, path)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil, err
		}
		header.Name = filepath.ToSlash(rel)
		workspaceEntry := taskWorkspaceTarEntry(header.Name)
		if info.IsDir() {
			header.Name += "/"
			if workspaceEntry {
				header.Mode = 0o777
			} else {
				header.Mode = 0o755
			}
		} else {
			if workspaceEntry {
				header.Mode = 0o666
				if info.Mode().Perm()&0o111 != 0 {
					header.Mode = 0o777
				}
			} else {
				header.Mode = 0o644
				if info.Mode().Perm()&0o111 != 0 {
					header.Mode = 0o755
				}
			}
		}
		header.Uid, header.Gid = 0, 0
		header.Uname, header.Gname = "", ""
		header.ModTime, header.AccessTime, header.ChangeTime = time.Unix(0, 0), time.Time{}, time.Time{}
		header.PAXRecords, header.Xattrs = nil, nil
		if err := tw.WriteHeader(header); err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			_, copyErr := io.Copy(tw, file)
			closeErr := file.Close()
			if copyErr != nil {
				return nil, copyErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func taskWorkspaceTarEntry(name string) bool {
	parts := strings.Split(filepath.ToSlash(strings.TrimSuffix(name, "/")), "/")
	return len(parts) >= 3 && parts[0] == "tasks" && parts[1] != "" && parts[2] == "workspace"
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
