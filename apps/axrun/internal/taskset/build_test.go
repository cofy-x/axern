package taskset

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestBuildIsDeterministicAndExpandsPerMatch(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "inputs", "a.txt"), "a\n", 0o644)
	mustWrite(t, filepath.Join(root, "inputs", "b.txt"), "b\n", 0o755)
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata:
  name: sample
spec:
  generators:
    - task_id_prefix: sample
      instruction:
        text: Process the input.
      workspace:
        paths: [inputs/*.txt]
        expand: per_match
      task:
        sandbox:
          backend: axern
          runtime_class: runsc
          workdir: /workspace
          runtime_source:
            type: image
            image: example.test/task@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        verifier:
          type: none
`
	mustWrite(t, filepath.Join(root, "taskset.yaml"), spec, 0o644)
	first, err := Build(BuildParams{File: filepath.Join(root, "taskset.yaml"), Output: filepath.Join(root, "one")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(BuildParams{File: filepath.Join(root, "taskset.yaml"), Output: filepath.Join(root, "two")})
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceDigest != second.SourceDigest || first.TaskCount != 2 {
		t.Fatalf("results first=%#v second=%#v", first, second)
	}
	a, _ := os.ReadFile(filepath.Join(root, "one", "payload.tar"))
	b, _ := os.ReadFile(filepath.Join(root, "two", "payload.tar"))
	if !bytes.Equal(a, b) {
		t.Fatal("deterministic payloads differ")
	}
	for _, relative := range []string{"oci-layout", "index.json"} {
		firstLayout, err := os.ReadFile(filepath.Join(root, "one", "oci", relative))
		if err != nil {
			t.Fatal(err)
		}
		secondLayout, err := os.ReadFile(filepath.Join(root, "two", "oci", relative))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(firstLayout, secondLayout) {
			t.Fatalf("OCI layout %s differs", relative)
		}
	}
	var descriptor Descriptor
	data, _ := os.ReadFile(filepath.Join(root, "one", "descriptor.json"))
	if err := json.Unmarshal(data, &descriptor); err != nil {
		t.Fatal(err)
	}
	if len(descriptor.Tasks) != 2 || descriptor.Tasks[0].Instance.ID >= descriptor.Tasks[1].Instance.ID {
		t.Fatalf("descriptor tasks = %#v", descriptor.Tasks)
	}
	if descriptor.Provenance.PackerVersion != PackerVersion {
		t.Fatalf("packer version = %q, want %q", descriptor.Provenance.PackerVersion, PackerVersion)
	}
	entries := tarEntries(t, a)
	if entries["tasks/"] == nil || entries["tasks/"].Mode&0o777 == 0 || entries["tasks/sample-inputs-b-"] != nil {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	foundExecutable := false
	foundWritableFile := false
	foundWritableDirectory := false
	for name, header := range entries {
		if strings.Contains(name, "/workspace/") && strings.HasSuffix(name, "b.txt") && header.Mode&0o777 == 0o777 {
			foundExecutable = true
		}
		if strings.Contains(name, "/workspace/") && strings.HasSuffix(name, "a.txt") && header.Mode&0o777 == 0o666 {
			foundWritableFile = true
		}
		if strings.HasSuffix(name, "/workspace/") && header.Mode&0o777 == 0o777 {
			foundWritableDirectory = true
		}
		if !strings.Contains(name, "/workspace/") && !strings.HasSuffix(name, "/workspace") && header.Mode&0o002 != 0 {
			t.Fatalf("protected payload entry %q is world-writable: mode=%#o", name, header.Mode)
		}
		if header.Uid != 0 || header.Gid != 0 {
			t.Fatalf("entry %q ownership = %d:%d, want deterministic root ownership", name, header.Uid, header.Gid)
		}
	}
	if !foundExecutable {
		t.Fatal("workspace executable was not made writable while preserving its executable bit")
	}
	if !foundWritableFile || !foundWritableDirectory {
		t.Fatalf("workspace writable contract missing: file=%v directory=%v", foundWritableFile, foundWritableDirectory)
	}
}

func TestBuildRejectsUnknownFieldAndEscapingPath(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "input.txt"), "x", 0o644)
	base := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: bad}
spec:
  generators:
    - task_id: bad
      instruction: {text: x}
      workspace: {paths: [%s], expand: aggregate}
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
%s`
	for name, test := range map[string][3]string{
		"escape":  {"../outside", "", "escapes the build root"},
		"unknown": {"input.txt", "      mystery: true\n", "field mystery"},
	} {
		t.Run(name, func(t *testing.T) {
			spec := filepath.Join(root, name+".yaml")
			value := strings.Replace(base, "%s", test[0], 1)
			value = strings.Replace(value, "%s", test[1], 1)
			mustWrite(t, spec, value, 0o644)
			_, err := Build(BuildParams{File: spec, Output: filepath.Join(root, "out-"+name)})
			if err == nil || !strings.Contains(err.Error(), test[2]) {
				t.Fatalf("error = %v, want %q", err, test[2])
			}
		})
	}
}

func TestPerMatchTaskIDIsBoundedAndPrefixCannotEscape(t *testing.T) {
	root := t.TempDir()
	longName := strings.Repeat("long-segment-", 12) + "input.txt"
	mustWrite(t, filepath.Join(root, longName), "x", 0o644)
	base := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: ids}
spec:
  generators:
    - task_id_prefix: %s
      instruction: {text: x}
      workspace: {paths: [%s], expand: per_match}
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
`
	badSpec := fmt.Sprintf(base, "../escape", longName)
	mustWrite(t, filepath.Join(root, "bad.yaml"), badSpec, 0o644)
	if _, err := Build(BuildParams{
		File:   filepath.Join(root, "bad.yaml"),
		Output: filepath.Join(root, "bad-out"),
	}); err == nil || !strings.Contains(err.Error(), "task_id_prefix") {
		t.Fatalf("escaping prefix error = %v", err)
	}
	goodSpec := fmt.Sprintf(base, "sample", longName)
	mustWrite(t, filepath.Join(root, "good.yaml"), goodSpec, 0o644)
	result, err := Build(BuildParams{File: filepath.Join(root, "good.yaml"), Output: filepath.Join(root, "good-out")})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(resolved.Tasks[0].ID); got > contract.MaxPathSegmentBytes {
		t.Fatalf("generated task id is %d bytes: %q", got, resolved.Tasks[0].ID)
	}
}

func TestTaskSetRejectsInvalidOrIgnoredExecutionContracts(t *testing.T) {
	base := TaskTemplate{
		Sandbox:  domain.SandboxSpec{Backend: domain.SandboxBackendLocal, Workdir: "/workspace"},
		Verifier: domain.VerifierSpec{Type: domain.VerifierTypeNone},
	}
	tests := map[string]struct {
		mutate func(*TaskTemplate)
		want   string
	}{
		"sandbox resources": {
			mutate: func(task *TaskTemplate) { task.Sandbox.Resources = &domain.ResourceSpec{RequestCPU: "1"} },
			want:   "use task.resources",
		},
		"invalid verifier": {
			mutate: func(task *TaskTemplate) { task.Verifier = domain.VerifierSpec{Type: domain.VerifierTypeShell} },
			want:   "command is required",
		},
		"negative timeout": {
			mutate: func(task *TaskTemplate) { task.Timeouts = &domain.TimeoutPolicy{EpisodeSec: -1} },
			want:   "must be non-negative",
		},
		"invalid cpu": {
			mutate: func(task *TaskTemplate) { task.Resources = &domain.ResourceSpec{RequestCPU: "invalid"} },
			want:   "task.resources",
		},
		"unsupported disk": {
			mutate: func(task *TaskTemplate) { task.Resources = &domain.ResourceSpec{Disk: "10Gi"} },
			want:   "ephemeral workspace disk contract",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			task := base
			test.mutate(&task)
			if err := validateTaskTemplate(task); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateTaskTemplate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDescriptorImageUsesTypedJSONLayer(t *testing.T) {
	want := []byte(`{"api_version":"axrun/v1"}`)
	image, err := newDescriptorImage(want)
	if err != nil {
		t.Fatal(err)
	}
	mediaType, err := image.MediaType()
	if err != nil || mediaType != types.OCIManifestSchema1 {
		t.Fatalf("manifest media type = %q, err = %v", mediaType, err)
	}
	layers, err := image.Layers()
	if err != nil || len(layers) != 1 {
		t.Fatalf("layers = %d, err = %v", len(layers), err)
	}
	layerType, err := layers[0].MediaType()
	if err != nil || layerType != types.MediaType(DescriptorMediaType) {
		t.Fatalf("layer media type = %q, err = %v", layerType, err)
	}
	reader, err := layers[0].Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("descriptor bytes = %q, err = %v", got, err)
	}
}

func TestDescriptorManifestEnvelopeSupport(t *testing.T) {
	for _, mediaType := range []types.MediaType{types.OCIManifestSchema1, types.DockerManifestSchema2} {
		if !supportedDescriptorMediaType(mediaType) {
			t.Fatalf("descriptor image envelope %q should be supported", mediaType)
		}
	}
	for _, mediaType := range []types.MediaType{types.OCIImageIndex, types.DockerManifestList, types.OCIConfigJSON} {
		if supportedDescriptorMediaType(mediaType) {
			t.Fatalf("non-image descriptor envelope %q should be rejected", mediaType)
		}
	}
}

func TestDescriptorLayerEnvelopeSupport(t *testing.T) {
	for _, mediaType := range []types.MediaType{types.MediaType(DescriptorMediaType), types.DockerLayer} {
		if !supportedDescriptorLayerMediaType(mediaType) {
			t.Fatalf("descriptor layer envelope %q should be supported", mediaType)
		}
	}
	for _, mediaType := range []types.MediaType{types.OCILayer, types.DockerForeignLayer, types.OCIConfigJSON} {
		if supportedDescriptorLayerMediaType(mediaType) {
			t.Fatalf("unsupported descriptor layer envelope %q should be rejected", mediaType)
		}
	}
}

func TestDecodeRegistryNormalizedDescriptorLayer(t *testing.T) {
	want := []byte(`{"api_version":"axrun/v1","kind":"TaskSet"}`)
	var layer bytes.Buffer
	writer := tar.NewWriter(&layer)
	if err := writer.WriteHeader(&tar.Header{Name: "descriptor.json", Mode: 0o644, Size: int64(len(want)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := decodeDescriptorLayer(types.DockerLayer, layer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("descriptor = %q, want %q", got, want)
	}
}

func TestDecodeRegistryNormalizedDescriptorLayerRejectsAdditionalEntries(t *testing.T) {
	var layer bytes.Buffer
	writer := tar.NewWriter(&layer)
	for _, name := range []string{"descriptor.json", "unexpected.json"} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeDescriptorLayer(types.DockerLayer, layer.Bytes()); err == nil || !strings.Contains(err.Error(), "additional entries") {
		t.Fatalf("decodeDescriptorLayer() error = %v", err)
	}
}

func TestResolveDescriptorTasksDoesNotMutateCanonicalDescriptor(t *testing.T) {
	descriptor := Descriptor{Tasks: []Task{{
		Instance: domain.TaskInstance{
			ID:           "example",
			InitialState: &domain.InitialStateSpec{Type: "directory", Path: "tasks/example/workspace"},
			Verifier:     domain.VerifierSpec{Assets: []domain.VerifierAssetSpec{{Path: "tasks/example/verifier/check.sh"}}},
			Oracle:       &domain.OracleSpec{Path: "tasks/example/oracle/answer.txt"},
		},
		WorkspaceSubpath: "tasks/example/workspace",
	}}}
	instances := resolveDescriptorTasks(descriptor, "/bundle")
	if got := descriptor.Tasks[0].Instance.Verifier.Assets[0].Path; got != "tasks/example/verifier/check.sh" {
		t.Fatalf("descriptor verifier asset mutated to %q", got)
	}
	if got := descriptor.Tasks[0].Instance.InitialState.Path; got != "tasks/example/workspace" {
		t.Fatalf("descriptor initial state mutated to %q", got)
	}
	if got := descriptor.Tasks[0].Instance.Oracle.Path; got != "tasks/example/oracle/answer.txt" {
		t.Fatalf("descriptor oracle mutated to %q", got)
	}
	if got := instances[0].Verifier.Assets[0].Path; got != "/bundle/payload/tasks/example/verifier/check.sh" {
		t.Fatalf("resolved verifier asset = %q", got)
	}
}

func TestBuildRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "real.txt"), "x", 0o644)
	if err := os.Symlink("real.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Skip(err)
	}
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: bad}
spec:
  generators:
    - task_id: bad
      instruction: {text: x}
      workspace: {paths: [link.txt], expand: aggregate}
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
`
	mustWrite(t, filepath.Join(root, "taskset.yaml"), spec, 0o644)
	_, err := Build(BuildParams{File: filepath.Join(root, "taskset.yaml"), Output: filepath.Join(root, "out")})
	if err == nil || !strings.Contains(err.Error(), "symlink is not allowed") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildWorkspaceDotDoesNotCaptureBuildStaging(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "input.txt"), "x", 0o644)
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: dot}
spec:
  generators:
    - task_id: dot
      instruction: {text: x}
      workspace: {paths: [.], expand: aggregate}
      exclude_paths: [taskset.yaml, out]
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
`
	mustWrite(t, filepath.Join(root, "taskset.yaml"), spec, 0o644)
	if _, err := Build(BuildParams{File: filepath.Join(root, "taskset.yaml"), Output: filepath.Join(root, "out")}); err != nil {
		t.Fatal(err)
	}
	entries := tarEntries(t, mustRead(t, filepath.Join(root, "out", "payload.tar")))
	for name := range entries {
		if strings.Contains(name, ".axrun-taskset-") || strings.Contains(name, "/out/") {
			t.Fatalf("payload captured build staging entry %q", name)
		}
	}
}

func TestBuildRejectsSymlinkedParentAndInvalidExclude(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "input.txt"), "outside", 0o644)
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skip(err)
	}
	base := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: bad}
spec:
  generators:
    - task_id: bad
      instruction: {text: x}
      workspace: {paths: [%s], expand: aggregate}
      exclude_paths: [%s]
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
`
	for name, tc := range map[string]struct{ path, exclude, want string }{
		"linked-parent": {"linked/input.txt", "none", "symlink is not allowed"},
		"bad-exclude":   {"taskset.yaml", "'[unterminated'", "pattern"},
	} {
		t.Run(name, func(t *testing.T) {
			spec := fmt.Sprintf(base, tc.path, tc.exclude)
			path := filepath.Join(root, name+".yaml")
			mustWrite(t, path, spec, 0o644)
			_, err := Build(BuildParams{File: path, Output: filepath.Join(root, "out-"+name)})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestResolveRejectsTamperedLocalPayload(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "input.txt"), "original", 0o644)
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: tamper}
spec:
  generators:
    - task_id: tamper
      instruction: {text: x}
      workspace: {paths: [input.txt], expand: aggregate}
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
`
	mustWrite(t, filepath.Join(root, "taskset.yaml"), spec, 0o644)
	bundle := filepath.Join(root, "bundle")
	if _, err := Build(BuildParams{File: filepath.Join(root, "taskset.yaml"), Output: bundle}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(bundle, "payload", "tasks", "tamper", "workspace", "input.txt"), "changed", 0o644)
	if _, err := Resolve(bundle); err == nil || !strings.Contains(err.Error(), "does not match payload.tar") {
		t.Fatalf("error = %v", err)
	}
}

func TestPerMatchIDChangesWithContent(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.txt")
	mustWrite(t, input, "first", 0o644)
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: content-id}
spec:
  generators:
    - task_id_prefix: case
      instruction: {text: x}
      workspace: {paths: [input.txt], expand: per_match}
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
`
	mustWrite(t, filepath.Join(root, "taskset.yaml"), spec, 0o644)
	if _, err := Build(BuildParams{File: filepath.Join(root, "taskset.yaml"), Output: filepath.Join(root, "one")}); err != nil {
		t.Fatal(err)
	}
	first := readDescriptor(t, filepath.Join(root, "one", "descriptor.json"))
	mustWrite(t, input, "second", 0o644)
	if _, err := Build(BuildParams{File: filepath.Join(root, "taskset.yaml"), Output: filepath.Join(root, "two")}); err != nil {
		t.Fatal(err)
	}
	second := readDescriptor(t, filepath.Join(root, "two", "descriptor.json"))
	if first.Tasks[0].Instance.ID == second.Tasks[0].Instance.ID {
		t.Fatalf("content change did not change generated task id %q", first.Tasks[0].Instance.ID)
	}
}

func TestSourceDigestIncludesCanonicalTaskContract(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "input.txt"), "unchanged", 0o644)
	writeSpec := func(instruction, name string) string {
		spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: task-contract}
spec:
  generators:
    - task_id: fixed
      instruction: {text: ` + instruction + `}
      workspace: {paths: [input.txt], expand: aggregate}
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
`
		path := filepath.Join(root, name+".yaml")
		mustWrite(t, path, spec, 0o644)
		return path
	}
	first, err := Build(BuildParams{File: writeSpec("first", "first"), Output: filepath.Join(root, "one")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(BuildParams{File: writeSpec("second", "second"), Output: filepath.Join(root, "two")})
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceDigest == second.SourceDigest {
		t.Fatalf("task contract change did not change source digest %q", first.SourceDigest)
	}
}

func TestBuildRejectsHardlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	link := filepath.Join(root, "linked.txt")
	mustWrite(t, real, "x", 0o644)
	if err := os.Link(real, link); err != nil {
		t.Skip(err)
	}
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: hardlink}
spec:
  generators:
    - task_id: hardlink
      instruction: {text: x}
      workspace: {paths: [linked.txt], expand: aggregate}
      task:
        sandbox: {backend: local, runtime_class: runsc, workdir: /workspace}
        verifier: {type: none}
`
	mustWrite(t, filepath.Join(root, "taskset.yaml"), spec, 0o644)
	_, err := Build(BuildParams{File: filepath.Join(root, "taskset.yaml"), Output: filepath.Join(root, "out")})
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("error = %v", err)
	}
}

func readDescriptor(t *testing.T, path string) Descriptor {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var descriptor Descriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		t.Fatal(err)
	}
	return descriptor
}

func mustWrite(t *testing.T, path, value string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func tarEntries(t *testing.T, data []byte) map[string]*tar.Header {
	t.Helper()
	entries := map[string]*tar.Header{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		copy := *header
		entries[header.Name] = &copy
	}
}
