package executionkernel

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNormalizeConfigForRootfsValidatesAndOrdersDeclaredOutputs(t *testing.T) {
	config, err := NormalizeConfigForRootfs(&commonv1.ExecutionConfig{DeclaredOutputs: []*commonv1.DeclaredOutput{
		{Path: " /workspace/result/ ", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR, MediaType: " application/x-tar "},
		{Path: "/workspace/candidate.patch", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE, MediaType: "text/x-diff"},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.GetDeclaredOutputs(); len(got) != 2 || got[0].GetPath() != "/workspace/candidate.patch" || got[1].GetPath() != "/workspace/result" {
		t.Fatalf("declared outputs = %+v", got)
	}
}

func TestNormalizeConfigForRootfsRejectsUnsafeDeclaredOutputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		output *commonv1.DeclaredOutput
	}{
		{name: "relative", output: &commonv1.DeclaredOutput{Path: "result", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE}},
		{name: "parent", output: &commonv1.DeclaredOutput{Path: "/workspace/../secret", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE}},
		{name: "root", output: &commonv1.DeclaredOutput{Path: "/", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR}},
		{name: "format", output: &commonv1.DeclaredOutput{Path: "/workspace/result"}},
		{name: "media", output: &commonv1.DeclaredOutput{Path: "/workspace/result", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE, MediaType: "not a media type"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeConfigForRootfs(&commonv1.ExecutionConfig{DeclaredOutputs: []*commonv1.DeclaredOutput{test.output}}, false)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestNormalizeConfigForRootfsRejectsDuplicateDeclaredOutput(t *testing.T) {
	_, err := NormalizeConfigForRootfs(&commonv1.ExecutionConfig{DeclaredOutputs: []*commonv1.DeclaredOutput{
		{Path: "/workspace/result", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE},
		{Path: "/workspace/./result", Format: commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE},
	}}, false)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v", err)
	}
}
