package output

import (
	"io"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

type EnvironmentListJSON struct {
	Environments []*EnvironmentJSON `json:"environments"`
	NextCursor   string             `json:"next_cursor,omitempty"`
}

type EnvironmentResponseJSON struct {
	Environment *EnvironmentJSON `json:"environment"`
}

type EnvironmentJSON struct {
	ID               string               `json:"id"`
	Namespace        string               `json:"namespace"`
	Status           string               `json:"status"`
	Spec             *EnvironmentSpecJSON `json:"spec,omitempty"`
	SpecHash         string               `json:"spec_hash,omitempty"`
	ResolvedTemplate *RuntimeTemplateJSON `json:"resolved_template,omitempty"`
	Labels           map[string]string    `json:"labels,omitempty"`
	Version          int64                `json:"version"`
	CreatedAt        string               `json:"created_at,omitempty"`
	UpdatedAt        string               `json:"updated_at,omitempty"`
	Message          string               `json:"message,omitempty"`
}

type EnvironmentSpecJSON struct {
	Namespace       string                      `json:"namespace,omitempty"`
	TemplateID      string                      `json:"template_id,omitempty"`
	TemplateVersion string                      `json:"template_version,omitempty"`
	Image           *EnvironmentImageSourceJSON `json:"image,omitempty"`
}

type EnvironmentImageSourceJSON struct {
	Ref                  string `json:"ref,omitempty"`
	Digest               string `json:"digest,omitempty"`
	RootfsReadonly       bool   `json:"rootfs_readonly,omitempty"`
	RegistryCredentialID string `json:"registry_credential_id,omitempty"`
}

func PrintEnvironmentListJSON(w io.Writer, resp *environmentv1.ListEnvironmentsResponse) error {
	out := EnvironmentListJSON{}
	if resp != nil {
		out.NextCursor = resp.GetNextCursor()
		out.Environments = make([]*EnvironmentJSON, 0, len(resp.GetEnvironments()))
		for _, environment := range resp.GetEnvironments() {
			out.Environments = append(out.Environments, NewEnvironmentJSON(environment))
		}
	}
	return PrintJSON(w, out)
}

func PrintEnvironmentResponseJSON(w io.Writer, environment *environmentv1.Environment) error {
	return PrintJSON(w, EnvironmentResponseJSON{Environment: NewEnvironmentJSON(environment)})
}

func NewEnvironmentJSON(environment *environmentv1.Environment) *EnvironmentJSON {
	if environment == nil {
		return nil
	}
	return &EnvironmentJSON{
		ID:               environment.GetID(),
		Namespace:        environment.GetNamespace(),
		Status:           EnvironmentStatusLabel(environment.GetStatus()),
		Spec:             newEnvironmentSpecJSON(environment.GetSpec()),
		SpecHash:         environment.GetSpecHash(),
		ResolvedTemplate: NewRuntimeTemplateJSON(environment.GetResolvedTemplate()),
		Labels:           cloneStringMap(environment.GetLabels()),
		Version:          environment.GetVersion(),
		CreatedAt:        FormatProtoTimestamp(environment.GetCreatedAt()),
		UpdatedAt:        FormatProtoTimestamp(environment.GetUpdatedAt()),
		Message:          environment.GetMessage(),
	}
}

func newEnvironmentSpecJSON(spec *environmentv1.EnvironmentSpec) *EnvironmentSpecJSON {
	if spec == nil {
		return nil
	}
	return &EnvironmentSpecJSON{
		Namespace:       spec.GetNamespace(),
		TemplateID:      spec.GetTemplateID(),
		TemplateVersion: spec.GetTemplateVersion(),
		Image:           newEnvironmentImageSourceJSON(spec.GetImage()),
	}
}

func newEnvironmentImageSourceJSON(image *environmentv1.EnvironmentImageSource) *EnvironmentImageSourceJSON {
	if image == nil {
		return nil
	}
	return &EnvironmentImageSourceJSON{
		Ref:                  image.GetRef(),
		Digest:               image.GetDigest(),
		RootfsReadonly:       image.GetRootfsReadonly(),
		RegistryCredentialID: image.GetRegistryCredentialID(),
	}
}
