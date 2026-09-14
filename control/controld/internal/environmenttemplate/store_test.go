package environmenttemplate

import (
	"strings"
	"testing"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	privateenvironmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/environment/v1"
)

func TestListIncludesPython311(t *testing.T) {
	store := NewStore(nil)

	templates := store.List()
	if len(templates) != 4 {
		t.Fatalf("environment template count = %d, want 4", len(templates))
	}
	var got *privateenvironmentv1.EnvironmentTemplate
	for _, template := range templates {
		if template.GetID() == "python311" {
			got = template
			break
		}
	}
	if got == nil {
		t.Fatal("environment template python311 is missing")
	}
	if got.GetID() != "python311" {
		t.Fatalf("environment template id = %q, want python311", got.GetID())
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetDigest() == "" {
		t.Fatal("environment template image_descriptor is empty")
	}
	if len(got.GetResolvedSpec().GetImageDefaultArgv()) != 1 || got.GetResolvedSpec().GetImageDefaultArgv()[0] != "python3" {
		t.Fatalf("python311 image_default_argv = %#v, want python3", got.GetResolvedSpec().GetImageDefaultArgv())
	}
	if got.GetResolvedSpec().GetDefaultCwd() != "/workspace" {
		t.Fatalf("python311 default_cwd = %q, want /workspace", got.GetResolvedSpec().GetDefaultCwd())
	}
	if got.GetResolvedSpec().GetExecutionProfile().GetBaseline().GetNoFileLimit() != 1048576 {
		t.Fatalf("python311 execution profile nofile = %d, want 1048576", got.GetResolvedSpec().GetExecutionProfile().GetBaseline().GetNoFileLimit())
	}
}

func TestListIncludesServerBase(t *testing.T) {
	store := NewStore(nil)

	got, ok := store.Get("server-base", "")
	if !ok {
		t.Fatal("Get(server-base) ok = false, want true")
	}
	if got.GetVersion() != "24.04.0" {
		t.Fatalf("server-base version = %q, want 24.04.0", got.GetVersion())
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetDigest() == "" {
		t.Fatal("server-base image_descriptor is empty")
	}
	if len(got.GetResolvedSpec().GetImageDefaultArgv()) != 3 || got.GetResolvedSpec().GetImageDefaultArgv()[0] != "/usr/bin/supervisord" {
		t.Fatalf("server-base image_default_argv = %#v, want supervisord command", got.GetResolvedSpec().GetImageDefaultArgv())
	}
	if got.GetResolvedSpec().GetDefaultCwd() != "/home/axern" {
		t.Fatalf("server-base default_cwd = %q, want /home/axern", got.GetResolvedSpec().GetDefaultCwd())
	}
}

func TestListIncludesCodingBase(t *testing.T) {
	store := NewStore(nil)

	got, ok := store.Get("coding-base", "")
	if !ok {
		t.Fatal("Get(coding-base) ok = false, want true")
	}
	if got.GetVersion() != "24.04.0" {
		t.Fatalf("coding-base version = %q, want 24.04.0", got.GetVersion())
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"] != "ghcr.io/cofy-x/axern/coding-base-runtime:24.04" {
		t.Fatalf("coding-base image ref = %q", got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"])
	}
	if len(got.GetResolvedSpec().GetImageDefaultArgv()) != 3 || got.GetResolvedSpec().GetImageDefaultArgv()[0] != "/usr/bin/supervisord" {
		t.Fatalf("coding-base image_default_argv = %#v, want supervisord command", got.GetResolvedSpec().GetImageDefaultArgv())
	}
	if got.GetResolvedSpec().GetDefaultCwd() != "/home/axern" {
		t.Fatalf("coding-base default_cwd = %q, want /home/axern", got.GetResolvedSpec().GetDefaultCwd())
	}
}

func TestListIncludesDesktopBase(t *testing.T) {
	store := NewStore(nil)

	got, ok := store.Get("desktop-base", "")
	if !ok {
		t.Fatal("Get(desktop-base) ok = false, want true")
	}
	if got.GetVersion() != "24.04.0" {
		t.Fatalf("desktop-base version = %q, want 24.04.0", got.GetVersion())
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"] != "ghcr.io/cofy-x/axern/desktop-base-runtime:24.04" {
		t.Fatalf("desktop-base image ref = %q", got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"])
	}
	if got.GetResolvedSpec().GetDefaultEnv()["AXERN_SANDBOXD_COMPUTER_USE"] != "1" || got.GetResolvedSpec().GetDefaultEnv()["DISPLAY"] != ":99" {
		t.Fatalf("desktop-base default env = %#v", got.GetResolvedSpec().GetDefaultEnv())
	}
}

func TestGetReturnsNotFoundForUnknownID(t *testing.T) {
	store := NewStore(nil)

	if _, ok := store.Get("missing", ""); ok {
		t.Fatal("Get() ok = true, want false")
	}
}

func TestGetTemplateHonorsVersion(t *testing.T) {
	store := NewStore([]*privateenvironmentv1.EnvironmentTemplate{
		{ID: "python311", Version: "3.11.0", ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{ImageDescriptor: &environmentv1.OciImageDescriptor{Digest: "sha256:old"}}},
		{ID: "python311", Version: "3.11.1", ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{ImageDescriptor: &environmentv1.OciImageDescriptor{Digest: "sha256:new"}}},
	})

	got, ok := store.Get("python311", "3.11.0")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetDigest() != "sha256:old" {
		t.Fatalf("digest = %q, want sha256:old", got.GetResolvedSpec().GetImageDescriptor().GetDigest())
	}
	if _, ok := store.Get("python311", "missing"); ok {
		t.Fatal("Get(missing version) ok = true, want false")
	}
}

func TestDefaultPythonEnvironmentTemplateImageCanBeOverriddenByEnv(t *testing.T) {
	const override = "host.docker.internal:35000/axern/python311-runtime:dev"

	t.Setenv("AXERN_RUNTIME_TEMPLATE_PYTHON311_IMAGE", override)

	store := NewStore(nil)
	got, ok := store.Get("python311", "")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"] != override {
		t.Fatalf("environment template image ref = %q, want %q", got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"], override)
	}
}

func TestDefaultServerBaseEnvironmentTemplateImageCanBeOverriddenByEnv(t *testing.T) {
	const override = "host.docker.internal:35000/axern/server-base-runtime:dev"

	t.Setenv("AXERN_RUNTIME_TEMPLATE_SERVER_BASE_IMAGE", override)

	store := NewStore(nil)
	got, ok := store.Get("server-base", "")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"] != override {
		t.Fatalf("environment template image ref = %q, want %q", got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"], override)
	}
}

func TestDefaultDesktopBaseEnvironmentTemplateImageCanBeOverriddenByEnv(t *testing.T) {
	const override = "host.docker.internal:35000/axern/desktop-base-runtime:dev"

	t.Setenv("AXERN_RUNTIME_TEMPLATE_DESKTOP_BASE_IMAGE", override)

	store := NewStore(nil)
	got, ok := store.Get("desktop-base", "")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"] != override {
		t.Fatalf("environment template image ref = %q, want %q", got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"], override)
	}
}

func TestParseDefaultTemplatesRejectsInvalidFixture(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "invalid-json",
			raw:     `{`,
			wantErr: "parse templates/environment_templates.json",
		},
		{
			name: "missing-id",
			raw: `[{
				"version": "1.0.0",
				"resolvedSpec": {"imageDescriptor": {"digest": "sha256:1", "annotations": {"org.opencontainers.image.ref.name": "example:1"}}, "executionProfile": {}}
			}]`,
			wantErr: "id is required",
		},
		{
			name: "missing-image-ref",
			raw: `[{
				"id": "example",
				"version": "1.0.0",
				"resolvedSpec": {"imageDescriptor": {"digest": "sha256:1"}, "executionProfile": {}}
			}]`,
			wantErr: "org.opencontainers.image.ref.name",
		},
		{
			name: "duplicate-template",
			raw: `[{
				"id": "example",
				"version": "1.0.0",
				"resolvedSpec": {"imageDescriptor": {"digest": "sha256:1", "annotations": {"org.opencontainers.image.ref.name": "example:1"}}, "executionProfile": {}}
			}, {
				"id": "example",
				"version": "1.0.0",
				"resolvedSpec": {"imageDescriptor": {"digest": "sha256:2", "annotations": {"org.opencontainers.image.ref.name": "example:2"}}, "executionProfile": {}}
			}]`,
			wantErr: "duplicate template example@1.0.0",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDefaultTemplates([]byte(tt.raw))
			if err == nil {
				t.Fatal("parseDefaultTemplates() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseDefaultTemplates() error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultEnvironmentTemplateImageOverrideDigestUpdatesDescriptorDigest(t *testing.T) {
	const override = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	t.Setenv("AXERN_RUNTIME_TEMPLATE_SERVER_BASE_IMAGE", override)

	store := NewStore(nil)
	got, ok := store.Get("server-base", "")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetDigest() != override {
		t.Fatalf("environment template digest = %q, want %q", got.GetResolvedSpec().GetImageDescriptor().GetDigest(), override)
	}
	if got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"] != override {
		t.Fatalf("environment template image ref = %q, want %q", got.GetResolvedSpec().GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"], override)
	}
}
