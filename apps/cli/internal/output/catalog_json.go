package output

import (
	"io"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

type EnvironmentTemplateListJSON struct {
	EnvironmentTemplates []*EnvironmentTemplateJSON `json:"environment_templates"`
}

type EnvironmentTemplateResponseJSON struct {
	EnvironmentTemplate *EnvironmentTemplateJSON `json:"environment_template"`
}

type EnvironmentTemplateJSON struct {
	ID              string                               `json:"id"`
	ResolvedSpec    *ResolvedEnvironmentSpecJSON         `json:"resolved_spec,omitempty"`
	Capabilities    *EnvironmentTemplateCapabilitiesJSON `json:"capabilities,omitempty"`
	Language        string                               `json:"language,omitempty"`
	LanguageVersion string                               `json:"language_version,omitempty"`
	Description     string                               `json:"description,omitempty"`
	Version         string                               `json:"version,omitempty"`
}

type ResolvedEnvironmentSpecJSON struct {
	RootfsReadonly   bool                    `json:"rootfs_readonly,omitempty"`
	ImageDefaultArgv []string                `json:"image_default_argv,omitempty"`
	DefaultCwd       string                  `json:"default_cwd,omitempty"`
	DefaultEnv       map[string]string       `json:"default_env,omitempty"`
	Mounts           []*EnvironmentMountJSON `json:"mounts,omitempty"`
	ImageDescriptor  *OciImageDescriptorJSON `json:"image_descriptor,omitempty"`
}

type EnvironmentMountJSON struct {
	Type    string   `json:"type,omitempty"`
	Source  string   `json:"source,omitempty"`
	Target  string   `json:"target,omitempty"`
	Options []string `json:"options,omitempty"`
}

type EnvironmentTemplateCapabilitiesJSON struct {
	SupportsExec             bool `json:"supports_exec,omitempty"`
	SupportsExecStream       bool `json:"supports_exec_stream,omitempty"`
	SupportsLongLivedProcess bool `json:"supports_long_lived_process,omitempty"`
	SupportsPorts            bool `json:"supports_ports,omitempty"`
}

type OciImageDescriptorJSON struct {
	Digest      string            `json:"digest,omitempty"`
	MediaType   string            `json:"media_type,omitempty"`
	SizeBytes   int64             `json:"size_bytes,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

func PrintEnvironmentTemplateListJSON(w io.Writer, resp *catalogv1.ListEnvironmentTemplatesResponse) error {
	out := EnvironmentTemplateListJSON{}
	if resp != nil {
		out.EnvironmentTemplates = make([]*EnvironmentTemplateJSON, 0, len(resp.GetEnvironmentTemplates()))
		for _, template := range resp.GetEnvironmentTemplates() {
			out.EnvironmentTemplates = append(out.EnvironmentTemplates, NewEnvironmentTemplateJSON(template))
		}
	}
	return PrintJSON(w, out)
}

func PrintEnvironmentTemplateResponseJSON(w io.Writer, resp *catalogv1.GetEnvironmentTemplateResponse) error {
	var template *catalogv1.EnvironmentTemplate
	if resp != nil {
		template = resp.GetEnvironmentTemplate()
	}
	return PrintJSON(w, EnvironmentTemplateResponseJSON{EnvironmentTemplate: NewEnvironmentTemplateJSON(template)})
}

func NewEnvironmentTemplateJSON(template *catalogv1.EnvironmentTemplate) *EnvironmentTemplateJSON {
	if template == nil {
		return nil
	}
	return &EnvironmentTemplateJSON{
		ID:              template.GetID(),
		ResolvedSpec:    newResolvedEnvironmentSpecJSON(template.GetResolvedSpec()),
		Capabilities:    newEnvironmentTemplateCapabilitiesJSON(template.GetCapabilities()),
		Language:        template.GetLanguage(),
		LanguageVersion: template.GetLanguageVersion(),
		Description:     template.GetDescription(),
		Version:         template.GetVersion(),
	}
}

func newResolvedEnvironmentSpecJSON(spec *catalogv1.ResolvedEnvironmentSpec) *ResolvedEnvironmentSpecJSON {
	if spec == nil {
		return nil
	}
	return &ResolvedEnvironmentSpecJSON{
		RootfsReadonly: spec.GetRootfsReadonly(), ImageDefaultArgv: append([]string(nil), spec.GetImageDefaultArgv()...),
		DefaultCwd: spec.GetDefaultCwd(), DefaultEnv: cloneStringMap(spec.GetDefaultEnv()),
		Mounts: newEnvironmentMountJSONs(spec.GetMounts()), ImageDescriptor: newOciImageDescriptorJSON(spec.GetImageDescriptor()),
	}
}

func newEnvironmentMountJSONs(mounts []*catalogv1.EnvironmentMount) []*EnvironmentMountJSON {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]*EnvironmentMountJSON, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		out = append(out, &EnvironmentMountJSON{
			Type:    mount.GetType(),
			Source:  mount.GetSource(),
			Target:  mount.GetTarget(),
			Options: append([]string(nil), mount.GetOptions()...),
		})
	}
	return out
}

func newEnvironmentTemplateCapabilitiesJSON(capabilities *catalogv1.EnvironmentTemplateCapabilities) *EnvironmentTemplateCapabilitiesJSON {
	if capabilities == nil {
		return nil
	}
	return &EnvironmentTemplateCapabilitiesJSON{
		SupportsExec:             capabilities.GetSupportsExec(),
		SupportsExecStream:       capabilities.GetSupportsExecStream(),
		SupportsLongLivedProcess: capabilities.GetSupportsLongLivedProcess(),
		SupportsPorts:            capabilities.GetSupportsPorts(),
	}
}

func newOciImageDescriptorJSON(descriptor *catalogv1.OciImageDescriptor) *OciImageDescriptorJSON {
	if descriptor == nil {
		return nil
	}
	return &OciImageDescriptorJSON{
		Digest:      descriptor.GetDigest(),
		MediaType:   descriptor.GetMediaType(),
		SizeBytes:   descriptor.GetSizeBytes(),
		Annotations: cloneStringMap(descriptor.GetAnnotations()),
	}
}
