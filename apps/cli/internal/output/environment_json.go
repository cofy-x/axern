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
	ID           string                       `json:"id"`
	Namespace    string                       `json:"namespace"`
	Spec         *EnvironmentSpecJSON         `json:"spec,omitempty"`
	ResolvedSpec *ResolvedEnvironmentSpecJSON `json:"resolved_spec,omitempty"`
	Labels       map[string]string            `json:"labels,omitempty"`
	CreatedAt    string                       `json:"created_at,omitempty"`
	DeletedAt    string                       `json:"deleted_at,omitempty"`
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
		ID: environment.GetID(), Namespace: environment.GetNamespace(),
		Spec:         newEnvironmentSpecJSON(environment.GetSpec()),
		ResolvedSpec: newResolvedEnvironmentSpecJSON(environment.GetResolvedSpec()),
		Labels:       cloneStringMap(environment.GetLabels()),
		CreatedAt:    FormatProtoTimestamp(environment.GetCreatedAt()),
		DeletedAt:    FormatProtoTimestamp(environment.GetDeletedAt()),
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
