package output

import (
	"io"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

type RuntimeTemplateListJSON struct {
	RuntimeTemplates []*RuntimeTemplateJSON `json:"runtime_templates"`
}

type RuntimeTemplateResponseJSON struct {
	RuntimeTemplate *RuntimeTemplateJSON `json:"runtime_template"`
}

type AgentBundleListJSON struct {
	AgentBundles []*AgentBundleJSON `json:"agent_bundles"`
}

type AgentBundleResponseJSON struct {
	AgentBundle *AgentBundleJSON `json:"agent_bundle"`
}

type AgentBundleJSON struct {
	ID              string                  `json:"id"`
	Version         string                  `json:"version"`
	BinaryPath      string                  `json:"binary_path"`
	Description     string                  `json:"description,omitempty"`
	ImageDescriptor *OciImageDescriptorJSON `json:"image_descriptor,omitempty"`
}

type RuntimeTemplateJSON struct {
	ID               string                           `json:"id"`
	RootfsReadonly   bool                             `json:"rootfs_readonly,omitempty"`
	ImageDefaultArgv []string                         `json:"image_default_argv,omitempty"`
	DefaultCwd       string                           `json:"default_cwd,omitempty"`
	DefaultEnv       map[string]string                `json:"default_env,omitempty"`
	Mounts           []*RuntimeMountJSON              `json:"mounts,omitempty"`
	Capabilities     *RuntimeTemplateCapabilitiesJSON `json:"capabilities,omitempty"`
	Language         string                           `json:"language,omitempty"`
	LanguageVersion  string                           `json:"language_version,omitempty"`
	Description      string                           `json:"description,omitempty"`
	Version          string                           `json:"version,omitempty"`
	ImageDescriptor  *OciImageDescriptorJSON          `json:"image_descriptor,omitempty"`
	WarmPolicy       string                           `json:"warm_policy,omitempty"`
	CachePolicy      string                           `json:"cache_policy,omitempty"`
}

type RuntimeMountJSON struct {
	Type    string   `json:"type,omitempty"`
	Source  string   `json:"source,omitempty"`
	Target  string   `json:"target,omitempty"`
	Options []string `json:"options,omitempty"`
}

type RuntimeTemplateCapabilitiesJSON struct {
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

func PrintRuntimeTemplateListJSON(w io.Writer, resp *catalogv1.ListRuntimeTemplatesResponse) error {
	out := RuntimeTemplateListJSON{}
	if resp != nil {
		out.RuntimeTemplates = make([]*RuntimeTemplateJSON, 0, len(resp.GetRuntimeTemplates()))
		for _, template := range resp.GetRuntimeTemplates() {
			out.RuntimeTemplates = append(out.RuntimeTemplates, NewRuntimeTemplateJSON(template))
		}
	}
	return PrintJSON(w, out)
}

func PrintRuntimeTemplateResponseJSON(w io.Writer, resp *catalogv1.GetRuntimeTemplateResponse) error {
	var template *catalogv1.RuntimeTemplate
	if resp != nil {
		template = resp.GetRuntimeTemplate()
	}
	return PrintJSON(w, RuntimeTemplateResponseJSON{RuntimeTemplate: NewRuntimeTemplateJSON(template)})
}

func PrintAgentBundleListJSON(w io.Writer, resp *catalogv1.ListAgentBundlesResponse) error {
	out := AgentBundleListJSON{AgentBundles: []*AgentBundleJSON{}}
	if resp != nil {
		for _, bundle := range resp.GetAgentBundles() {
			out.AgentBundles = append(out.AgentBundles, newAgentBundleJSON(bundle))
		}
	}
	return PrintJSON(w, out)
}

func PrintAgentBundleResponseJSON(w io.Writer, resp *catalogv1.GetAgentBundleResponse) error {
	var bundle *catalogv1.AgentBundle
	if resp != nil {
		bundle = resp.GetAgentBundle()
	}
	return PrintJSON(w, AgentBundleResponseJSON{AgentBundle: newAgentBundleJSON(bundle)})
}

func newAgentBundleJSON(bundle *catalogv1.AgentBundle) *AgentBundleJSON {
	if bundle == nil {
		return nil
	}
	return &AgentBundleJSON{
		ID: bundle.GetID(), Version: bundle.GetVersion(), BinaryPath: bundle.GetBinaryPath(), Description: bundle.GetDescription(),
		ImageDescriptor: newOciImageDescriptorJSON(bundle.GetImageDescriptor()),
	}
}

func NewRuntimeTemplateJSON(template *catalogv1.RuntimeTemplate) *RuntimeTemplateJSON {
	if template == nil {
		return nil
	}
	return &RuntimeTemplateJSON{
		ID:               template.GetID(),
		RootfsReadonly:   template.GetRootfsReadonly(),
		ImageDefaultArgv: append([]string(nil), template.GetImageDefaultArgv()...),
		DefaultCwd:       template.GetDefaultCwd(),
		DefaultEnv:       cloneStringMap(template.GetDefaultEnv()),
		Mounts:           newRuntimeMountJSONs(template.GetMounts()),
		Capabilities:     newRuntimeTemplateCapabilitiesJSON(template.GetCapabilities()),
		Language:         template.GetLanguage(),
		LanguageVersion:  template.GetLanguageVersion(),
		Description:      template.GetDescription(),
		Version:          template.GetVersion(),
		ImageDescriptor:  newOciImageDescriptorJSON(template.GetImageDescriptor()),
		WarmPolicy:       template.GetWarmPolicy(),
		CachePolicy:      template.GetCachePolicy(),
	}
}

func newRuntimeMountJSONs(mounts []*catalogv1.RuntimeMount) []*RuntimeMountJSON {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]*RuntimeMountJSON, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		out = append(out, &RuntimeMountJSON{
			Type:    mount.GetType(),
			Source:  mount.GetSource(),
			Target:  mount.GetTarget(),
			Options: append([]string(nil), mount.GetOptions()...),
		})
	}
	return out
}

func newRuntimeTemplateCapabilitiesJSON(capabilities *catalogv1.RuntimeTemplateCapabilities) *RuntimeTemplateCapabilitiesJSON {
	if capabilities == nil {
		return nil
	}
	return &RuntimeTemplateCapabilitiesJSON{
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
