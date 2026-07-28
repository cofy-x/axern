package output

import (
	"bytes"
	"strings"
	"testing"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

func TestRenderRuntimeTemplateTableShowsTemplateAndLanguageVersions(t *testing.T) {
	var b bytes.Buffer
	RenderRuntimeTemplateTable(&b, []*catalogv1.RuntimeTemplate{
		{
			ID:              "python311",
			Version:         "3.11.0",
			Language:        "python",
			LanguageVersion: "3.11",
			ImageDescriptor: &catalogv1.OciImageDescriptor{Digest: "sha256:0311"},
		},
		{
			ID:              "server-base",
			Version:         "24.04.0",
			ImageDescriptor: &catalogv1.OciImageDescriptor{Digest: "sha256:2404"},
		},
		{
			ID:              "claude-code",
			Version:         "24.04.0",
			ImageDescriptor: &catalogv1.OciImageDescriptor{Digest: "sha256:c0d3"},
		},
	})

	got := b.String()
	for _, want := range []string{
		"ID           VERSION  LANGUAGE  LANG_VERSION  IMAGE",
		"python311    3.11.0   python    3.11          sha256:0311",
		"server-base  24.04.0  -         -             sha256:2404",
		"claude-code  24.04.0  -         -             sha256:c0d3",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("catalog table = %q, want line containing %q", got, want)
		}
	}
}

func TestRenderRuntimeTemplateUsesDashForEmptyOptionalFields(t *testing.T) {
	var b bytes.Buffer
	RenderRuntimeTemplate(&b, &catalogv1.RuntimeTemplate{
		ID:               "server-base",
		Version:          "24.04.0",
		ImageDefaultArgv: []string{"/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"},
		ImageDescriptor:  &catalogv1.OciImageDescriptor{Digest: "sha256:2404"},
	})

	got := b.String()
	for _, want := range []string{
		"ID: server-base",
		"Version: 24.04.0",
		"Language: -",
		"Language Version: -",
		"Image Digest: sha256:2404",
		"Image Default Argv: /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("catalog detail = %q, want line containing %q", got, want)
		}
	}
}
