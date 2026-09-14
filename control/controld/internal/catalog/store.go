package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	defaultEnvironmentTemplatesPath = "templates/environment_templates.json"
	imageRefAnnotationKey           = "org.opencontainers.image.ref.name"
)

//go:embed templates/*.json
var defaultEnvironmentTemplatesFS embed.FS

var runtimeImageOverrideEnv = map[string]string{
	"python311":    "AXERN_RUNTIME_CATALOG_PYTHON311_IMAGE",
	"server-base":  "AXERN_RUNTIME_CATALOG_SERVER_BASE_IMAGE",
	"coding-base":  "AXERN_RUNTIME_CATALOG_CODING_BASE_IMAGE",
	"desktop-base": "AXERN_RUNTIME_CATALOG_DESKTOP_BASE_IMAGE",
}

type Store struct {
	environmentTemplates map[string]*catalogv1.EnvironmentTemplate
}

func NewStore(in []*catalogv1.EnvironmentTemplate) *Store {
	if len(in) == 0 {
		in = DefaultTemplates()
	}
	return &Store{environmentTemplates: cloneEnvironmentTemplateMap(in)}
}

func (s *Store) List(filter *catalogv1.ListEnvironmentTemplatesRequest) []*catalogv1.EnvironmentTemplate {
	keys := make([]string, 0, len(s.environmentTemplates))
	for key := range s.environmentTemplates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]*catalogv1.EnvironmentTemplate, 0, len(keys))
	for _, key := range keys {
		template := s.environmentTemplates[key]
		if !matchesFilter(template, filter) {
			continue
		}
		out = append(out, proto.Clone(template).(*catalogv1.EnvironmentTemplate))
	}
	return out
}

func (s *Store) Get(id, version string) (*catalogv1.EnvironmentTemplate, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false
	}
	if version = strings.TrimSpace(version); version != "" {
		template, ok := s.environmentTemplates[templateKey(id, version)]
		if !ok {
			return nil, false
		}
		return proto.Clone(template).(*catalogv1.EnvironmentTemplate), true
	}
	candidates := make([]*catalogv1.EnvironmentTemplate, 0)
	for _, template := range s.environmentTemplates {
		if template.GetID() == id {
			candidates = append(candidates, template)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].GetVersion() > candidates[j].GetVersion()
	})
	return proto.Clone(candidates[0]).(*catalogv1.EnvironmentTemplate), true
}

func DefaultTemplates() []*catalogv1.EnvironmentTemplate {
	templates, err := loadDefaultTemplates()
	if err != nil {
		panic(fmt.Sprintf("load default environment templates: %v", err))
	}
	return templates
}

func loadDefaultTemplates() ([]*catalogv1.EnvironmentTemplate, error) {
	data, err := defaultEnvironmentTemplatesFS.ReadFile(defaultEnvironmentTemplatesPath)
	if err != nil {
		return nil, err
	}
	return parseDefaultTemplates(data)
}

func parseDefaultTemplates(data []byte) ([]*catalogv1.EnvironmentTemplate, error) {
	var rawTemplates []json.RawMessage
	if err := json.Unmarshal(data, &rawTemplates); err != nil {
		return nil, fmt.Errorf("parse %s: %w", defaultEnvironmentTemplatesPath, err)
	}
	templates := make([]*catalogv1.EnvironmentTemplate, 0, len(rawTemplates))
	seen := make(map[string]struct{}, len(rawTemplates))
	for idx, raw := range rawTemplates {
		template := &catalogv1.EnvironmentTemplate{}
		if err := protojson.Unmarshal(raw, template); err != nil {
			return nil, fmt.Errorf("parse %s[%d]: %w", defaultEnvironmentTemplatesPath, idx, err)
		}
		if err := validateDefaultEnvironmentTemplate(idx, template, seen); err != nil {
			return nil, err
		}
		applyEnvironmentTemplateOverrides(template)
		templates = append(templates, template)
	}
	return templates, nil
}

func validateDefaultEnvironmentTemplate(idx int, template *catalogv1.EnvironmentTemplate, seen map[string]struct{}) error {
	if template.GetID() == "" {
		return fmt.Errorf("%s[%d]: id is required", defaultEnvironmentTemplatesPath, idx)
	}
	if template.GetVersion() == "" {
		return fmt.Errorf("%s[%d]: version is required", defaultEnvironmentTemplatesPath, idx)
	}
	key := templateKey(template.GetID(), template.GetVersion())
	if _, ok := seen[key]; ok {
		return fmt.Errorf("%s[%d]: duplicate template %s@%s", defaultEnvironmentTemplatesPath, idx, template.GetID(), template.GetVersion())
	}
	seen[key] = struct{}{}
	if template.GetResolvedSpec().GetImageDescriptor().GetDigest() == "" {
		return fmt.Errorf("%s[%d]: image_descriptor.digest is required", defaultEnvironmentTemplatesPath, idx)
	}
	if template.GetResolvedSpec().GetImageDescriptor().GetAnnotations()[imageRefAnnotationKey] == "" {
		return fmt.Errorf("%s[%d]: image descriptor %q annotation is required", defaultEnvironmentTemplatesPath, idx, imageRefAnnotationKey)
	}
	if template.GetCapabilities() == nil {
		return fmt.Errorf("%s[%d]: capabilities are required", defaultEnvironmentTemplatesPath, idx)
	}
	if template.GetResolvedSpec().GetExecutionProfile() == nil {
		return fmt.Errorf("%s[%d]: execution_profile is required", defaultEnvironmentTemplatesPath, idx)
	}
	return nil
}

func applyEnvironmentTemplateOverrides(template *catalogv1.EnvironmentTemplate) {
	if template == nil {
		return
	}
	envKey := runtimeImageOverrideEnv[template.GetID()]
	value := strings.TrimSpace(os.Getenv(envKey))
	if value == "" {
		return
	}
	if template.ResolvedSpec == nil {
		template.ResolvedSpec = &catalogv1.ResolvedEnvironmentSpec{}
	}
	if template.ResolvedSpec.ImageDescriptor == nil {
		template.ResolvedSpec.ImageDescriptor = &catalogv1.OciImageDescriptor{}
	}
	if template.ResolvedSpec.ImageDescriptor.Annotations == nil {
		template.ResolvedSpec.ImageDescriptor.Annotations = map[string]string{}
	}
	template.ResolvedSpec.ImageDescriptor.Annotations[imageRefAnnotationKey] = value
	if strings.HasPrefix(value, "sha256:") {
		template.ResolvedSpec.ImageDescriptor.Digest = value
	}
}

func cloneEnvironmentTemplateMap(in []*catalogv1.EnvironmentTemplate) map[string]*catalogv1.EnvironmentTemplate {
	out := make(map[string]*catalogv1.EnvironmentTemplate, len(in))
	for _, template := range in {
		if template == nil {
			continue
		}
		id := strings.TrimSpace(template.GetID())
		if id == "" {
			continue
		}
		out[templateKey(id, template.GetVersion())] = proto.Clone(template).(*catalogv1.EnvironmentTemplate)
	}
	return out
}

func templateKey(id, version string) string {
	return strings.TrimSpace(id) + "\x00" + strings.TrimSpace(version)
}

func matchesFilter(template *catalogv1.EnvironmentTemplate, filter *catalogv1.ListEnvironmentTemplatesRequest) bool {
	if template == nil || filter == nil {
		return true
	}
	if version := strings.TrimSpace(filter.GetVersion()); version != "" && template.GetVersion() != version {
		return false
	}
	if language := strings.TrimSpace(filter.GetLanguage()); language != "" && template.GetLanguage() != language {
		return false
	}
	return true
}
