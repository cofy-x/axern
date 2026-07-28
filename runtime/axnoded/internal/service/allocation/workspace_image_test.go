package allocation

import (
	"os"
	"path/filepath"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestValidateWorkspaceImageRejectsEscapesAndUnsupportedVariants(t *testing.T) {
	valid := &runtime.WorkspaceImageSource{Variants: []*runtime.WorkspaceImageVariant{{Format: "nydus", Image: "example/nydus@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, {Format: "oci", Image: "example/oci@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}, SourcePath: "tasks/task-a/workspace", Target: "/workspace"}
	if err := validateWorkspaceImage(valid); err != nil {
		t.Fatalf("valid: %v", err)
	}
	invalid := proto.Clone(valid).(*runtime.WorkspaceImageSource)
	invalid.SourcePath = "tasks/task-a/../oracle"
	if err := validateWorkspaceImage(invalid); err == nil {
		t.Fatal("escaping source path accepted")
	}
	invalid = proto.Clone(valid).(*runtime.WorkspaceImageSource)
	invalid.Variants = []*runtime.WorkspaceImageVariant{{Format: "stargz", Image: "example/image"}}
	if err := validateWorkspaceImage(invalid); err == nil {
		t.Fatal("unsupported variant accepted")
	}
	invalid = proto.Clone(valid).(*runtime.WorkspaceImageSource)
	invalid.Target = "/etc/axrun"
	if err := validateWorkspaceImage(invalid); err == nil {
		t.Fatal("protected target descendant accepted")
	}
	invalid = proto.Clone(valid).(*runtime.WorkspaceImageSource)
	invalid.SourcePath = "tasks/nested/task/workspace"
	if err := validateWorkspaceImage(invalid); err == nil {
		t.Fatal("nested task source accepted")
	}
	invalid = proto.Clone(valid).(*runtime.WorkspaceImageSource)
	invalid.Variants = []*runtime.WorkspaceImageVariant{valid.Variants[1], valid.Variants[1]}
	if err := validateWorkspaceImage(invalid); err == nil {
		t.Fatal("duplicate workspace format accepted")
	}
	invalid = proto.Clone(valid).(*runtime.WorkspaceImageSource)
	invalid.Variants = []*runtime.WorkspaceImageVariant{{Format: "oci", Image: "example/oci@sha256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"}}
	if err := validateWorkspaceImage(invalid); err == nil {
		t.Fatal("non-canonical workspace digest accepted")
	}
}

func TestWorkspacePreparationReturnsIsolatedNodeObservedFacts(t *testing.T) {
	controller := &Controller{allocationStates: map[string]*allocationState{
		"attempt-a": {workspace: workspaceImageRecord{preparation: &commonv1.WorkspacePreparationFacts{
			PayloadFormat: "nydus",
			PayloadDigest: "sha256:payload",
			CacheHit:      true,
			CowPrepareMs:  9,
		}}},
	}}

	first := controller.WorkspacePreparation("attempt-a")
	if first.GetPayloadFormat() != "nydus" || first.GetPayloadDigest() != "sha256:payload" || !first.GetCacheHit() || first.GetCowPrepareMs() != 9 {
		t.Fatalf("workspace preparation = %+v", first)
	}
	first.PayloadFormat = "mutated"
	if got := controller.WorkspacePreparation("attempt-a").GetPayloadFormat(); got != "nydus" {
		t.Fatalf("stored preparation mutated through result: %q", got)
	}
}

func TestMaterializeTaskAssetsIsPhaseScopedAndAttemptIsolated(t *testing.T) {
	payload := t.TempDir()
	verifier := filepath.Join(payload, "tasks", "task-a", "verifier")
	oracle := filepath.Join(payload, "tasks", "task-a", "oracle")
	if err := os.MkdirAll(verifier, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(oracle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(verifier, "check.sh"), []byte("check\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oracle, "solution.txt"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mergedA, mergedB := t.TempDir(), t.TempDir()
	controller := &Controller{allocationStates: map[string]*allocationState{
		"attempt-a": {workspace: workspaceImageRecord{payloadRoot: payload, taskRoot: "tasks/task-a", merged: mergedA, target: "/workspace"}},
		"attempt-b": {workspace: workspaceImageRecord{payloadRoot: payload, taskRoot: "tasks/task-a", merged: mergedB, target: "/workspace"}},
	}}
	if _, err := os.Stat(filepath.Join(mergedA, ".axrun", "check.sh")); !os.IsNotExist(err) {
		t.Fatalf("verifier visible before phase: %v", err)
	}
	if _, err := controller.MaterializeTaskAssets("attempt-a", "tasks/task-a/verifier/check.sh", "/workspace/.axrun/check.sh", runtime.TaskAssetKind_TASK_ASSET_KIND_VERIFIER); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mergedA, ".axrun", "check.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mergedB, ".axrun", "check.sh")); !os.IsNotExist(err) {
		t.Fatalf("asset leaked to other attempt: %v", err)
	}
	if _, err := controller.MaterializeTaskAssets("attempt-a", "tasks/task-a/oracle/solution.txt", "/workspace/solution.txt", runtime.TaskAssetKind_TASK_ASSET_KIND_VERIFIER); err == nil {
		t.Fatal("oracle accepted as verifier asset")
	}
	if err := os.Symlink(filepath.Join("..", "oracle", "solution.txt"), filepath.Join(verifier, "oracle-link")); err == nil {
		if _, err := controller.MaterializeTaskAssets("attempt-a", "tasks/task-a/verifier/oracle-link", "/workspace/oracle-link", runtime.TaskAssetKind_TASK_ASSET_KIND_VERIFIER); err == nil {
			t.Fatal("symlinked verifier asset was accepted")
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(mergedA, "escape")); err == nil {
		if _, err := controller.MaterializeTaskAssets("attempt-a", "tasks/task-a/verifier/check.sh", "/workspace/escape/check.sh", runtime.TaskAssetKind_TASK_ASSET_KIND_VERIFIER); err == nil {
			t.Fatal("symlinked materialization target was accepted")
		}
	}
}
