package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	defaultRuntimeTemplatesPath = "templates/runtime_templates.json"
	defaultAgentBundlesPath     = "templates/agent_bundles.json"
	imageRefAnnotationKey       = "org.opencontainers.image.ref.name"
)

//go:embed templates/*.json
var defaultRuntimeTemplatesFS embed.FS

var runtimeImageOverrideEnv = map[string]string{
	"python311":    "AXERN_RUNTIME_CATALOG_PYTHON311_IMAGE",
	"server-base":  "AXERN_RUNTIME_CATALOG_SERVER_BASE_IMAGE",
	"coding-base":  "AXERN_RUNTIME_CATALOG_CODING_BASE_IMAGE",
	"desktop-base": "AXERN_RUNTIME_CATALOG_DESKTOP_BASE_IMAGE",
}

var agentBundleImageOverrideEnv = map[string]string{
	"claude-code": "AXERN_AGENT_BUNDLE_CLAUDE_CODE_IMAGE",
	"codex":       "AXERN_AGENT_BUNDLE_CODEX_IMAGE",
}

type Store struct {
	runtimeTemplates map[string]*catalogv1.RuntimeTemplate
	agentBundles     map[string]*catalogv1.AgentBundle
}

func NewStore(in []*catalogv1.RuntimeTemplate) *Store {
	if len(in) == 0 {
		in = DefaultTemplates()
	}
	return &Store{
		runtimeTemplates: cloneRuntimeTemplateMap(in),
		agentBundles:     cloneAgentBundleMap(DefaultAgentBundles()),
	}
}

func (s *Store) List(filter *catalogv1.ListRuntimeTemplatesRequest) []*catalogv1.RuntimeTemplate {
	keys := make([]string, 0, len(s.runtimeTemplates))
	for key := range s.runtimeTemplates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]*catalogv1.RuntimeTemplate, 0, len(keys))
	for _, key := range keys {
		template := s.runtimeTemplates[key]
		if !matchesFilter(template, filter) {
			continue
		}
		out = append(out, proto.Clone(template).(*catalogv1.RuntimeTemplate))
	}
	return out
}

func (s *Store) Get(id, version string) (*catalogv1.RuntimeTemplate, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false
	}
	if version = strings.TrimSpace(version); version != "" {
		template, ok := s.runtimeTemplates[templateKey(id, version)]
		if !ok {
			return nil, false
		}
		return proto.Clone(template).(*catalogv1.RuntimeTemplate), true
	}
	candidates := make([]*catalogv1.RuntimeTemplate, 0)
	for _, template := range s.runtimeTemplates {
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
	return proto.Clone(candidates[0]).(*catalogv1.RuntimeTemplate), true
}

func (s *Store) ListAgentBundles(filter *catalogv1.ListAgentBundlesRequest) []*catalogv1.AgentBundle {
	keys := make([]string, 0, len(s.agentBundles))
	for key := range s.agentBundles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*catalogv1.AgentBundle, 0, len(keys))
	for _, key := range keys {
		bundle := s.agentBundles[key]
		if filter != nil && strings.TrimSpace(filter.GetVersion()) != "" && bundle.GetVersion() != strings.TrimSpace(filter.GetVersion()) {
			continue
		}
		out = append(out, proto.Clone(bundle).(*catalogv1.AgentBundle))
	}
	return out
}

func (s *Store) GetAgentBundle(id, version string) (*catalogv1.AgentBundle, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false
	}
	if version = strings.TrimSpace(version); version != "" {
		bundle, ok := s.agentBundles[templateKey(id, version)]
		if !ok {
			return nil, false
		}
		return proto.Clone(bundle).(*catalogv1.AgentBundle), true
	}
	candidates := make([]*catalogv1.AgentBundle, 0)
	for _, bundle := range s.agentBundles {
		if bundle.GetID() == id {
			candidates = append(candidates, bundle)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].GetVersion() > candidates[j].GetVersion() })
	return proto.Clone(candidates[0]).(*catalogv1.AgentBundle), true
}

func DefaultTemplates() []*catalogv1.RuntimeTemplate {
	templates, err := loadDefaultTemplates()
	if err != nil {
		panic(fmt.Sprintf("load default runtime templates: %v", err))
	}
	return templates
}

func DefaultAgentBundles() []*catalogv1.AgentBundle {
	bundles, err := loadDefaultAgentBundles()
	if err != nil {
		panic(fmt.Sprintf("load default agent bundles: %v", err))
	}
	return bundles
}

func loadDefaultTemplates() ([]*catalogv1.RuntimeTemplate, error) {
	data, err := defaultRuntimeTemplatesFS.ReadFile(defaultRuntimeTemplatesPath)
	if err != nil {
		return nil, err
	}
	return parseDefaultTemplates(data)
}

func loadDefaultAgentBundles() ([]*catalogv1.AgentBundle, error) {
	data, err := defaultRuntimeTemplatesFS.ReadFile(defaultAgentBundlesPath)
	if err != nil {
		return nil, err
	}
	return parseDefaultAgentBundles(data)
}

func parseDefaultAgentBundles(data []byte) ([]*catalogv1.AgentBundle, error) {
	var rawBundles []json.RawMessage
	if err := json.Unmarshal(data, &rawBundles); err != nil {
		return nil, fmt.Errorf("parse %s: %w", defaultAgentBundlesPath, err)
	}
	bundles := make([]*catalogv1.AgentBundle, 0, len(rawBundles))
	seen := make(map[string]struct{}, len(rawBundles))
	for idx, raw := range rawBundles {
		bundle := &catalogv1.AgentBundle{}
		if err := protojson.Unmarshal(raw, bundle); err != nil {
			return nil, fmt.Errorf("parse %s[%d]: %w", defaultAgentBundlesPath, idx, err)
		}
		if err := validateDefaultAgentBundle(idx, bundle, seen); err != nil {
			return nil, err
		}
		applyAgentBundleOverride(bundle)
		bundles = append(bundles, bundle)
	}
	return bundles, nil
}

func validateDefaultAgentBundle(idx int, bundle *catalogv1.AgentBundle, seen map[string]struct{}) error {
	if strings.TrimSpace(bundle.GetID()) == "" {
		return fmt.Errorf("%s[%d]: id is required", defaultAgentBundlesPath, idx)
	}
	if strings.TrimSpace(bundle.GetVersion()) == "" {
		return fmt.Errorf("%s[%d]: version is required", defaultAgentBundlesPath, idx)
	}
	key := templateKey(bundle.GetID(), bundle.GetVersion())
	if _, ok := seen[key]; ok {
		return fmt.Errorf("%s[%d]: duplicate agent bundle %s@%s", defaultAgentBundlesPath, idx, bundle.GetID(), bundle.GetVersion())
	}
	seen[key] = struct{}{}
	if bundle.GetImageDescriptor().GetDigest() == "" {
		return fmt.Errorf("%s[%d]: image_descriptor.digest is required", defaultAgentBundlesPath, idx)
	}
	if bundle.GetImageDescriptor().GetAnnotations()[imageRefAnnotationKey] == "" {
		return fmt.Errorf("%s[%d]: image descriptor %q annotation is required", defaultAgentBundlesPath, idx, imageRefAnnotationKey)
	}
	binaryPath := strings.TrimSpace(bundle.GetBinaryPath())
	if strings.Contains(binaryPath, "\x00") || !strings.HasPrefix(binaryPath, "/") || path.Clean(binaryPath) != binaryPath || binaryPath == "/" {
		return fmt.Errorf("%s[%d]: binary_path must be a clean absolute path", defaultAgentBundlesPath, idx)
	}
	return nil
}

func parseDefaultTemplates(data []byte) ([]*catalogv1.RuntimeTemplate, error) {
	var rawTemplates []json.RawMessage
	if err := json.Unmarshal(data, &rawTemplates); err != nil {
		return nil, fmt.Errorf("parse %s: %w", defaultRuntimeTemplatesPath, err)
	}
	templates := make([]*catalogv1.RuntimeTemplate, 0, len(rawTemplates))
	seen := make(map[string]struct{}, len(rawTemplates))
	for idx, raw := range rawTemplates {
		template := &catalogv1.RuntimeTemplate{}
		if err := protojson.Unmarshal(raw, template); err != nil {
			return nil, fmt.Errorf("parse %s[%d]: %w", defaultRuntimeTemplatesPath, idx, err)
		}
		if err := validateDefaultRuntimeTemplate(idx, template, seen); err != nil {
			return nil, err
		}
		applyRuntimeTemplateOverrides(template)
		templates = append(templates, template)
	}
	return templates, nil
}

func validateDefaultRuntimeTemplate(idx int, template *catalogv1.RuntimeTemplate, seen map[string]struct{}) error {
	if template.GetID() == "" {
		return fmt.Errorf("%s[%d]: id is required", defaultRuntimeTemplatesPath, idx)
	}
	if template.GetVersion() == "" {
		return fmt.Errorf("%s[%d]: version is required", defaultRuntimeTemplatesPath, idx)
	}
	key := templateKey(template.GetID(), template.GetVersion())
	if _, ok := seen[key]; ok {
		return fmt.Errorf("%s[%d]: duplicate template %s@%s", defaultRuntimeTemplatesPath, idx, template.GetID(), template.GetVersion())
	}
	seen[key] = struct{}{}
	if template.GetImageDescriptor().GetDigest() == "" {
		return fmt.Errorf("%s[%d]: image_descriptor.digest is required", defaultRuntimeTemplatesPath, idx)
	}
	if template.GetImageDescriptor().GetAnnotations()[imageRefAnnotationKey] == "" {
		return fmt.Errorf("%s[%d]: image descriptor %q annotation is required", defaultRuntimeTemplatesPath, idx, imageRefAnnotationKey)
	}
	if template.GetCapabilities() == nil {
		return fmt.Errorf("%s[%d]: capabilities are required", defaultRuntimeTemplatesPath, idx)
	}
	if template.GetExecutionProfile() == nil {
		return fmt.Errorf("%s[%d]: execution_profile is required", defaultRuntimeTemplatesPath, idx)
	}
	return nil
}

func applyRuntimeTemplateOverrides(template *catalogv1.RuntimeTemplate) {
	if template == nil {
		return
	}
	envKey := runtimeImageOverrideEnv[template.GetID()]
	value := strings.TrimSpace(os.Getenv(envKey))
	if value == "" {
		return
	}
	if template.ImageDescriptor == nil {
		template.ImageDescriptor = &catalogv1.OciImageDescriptor{}
	}
	if template.ImageDescriptor.Annotations == nil {
		template.ImageDescriptor.Annotations = map[string]string{}
	}
	template.ImageDescriptor.Annotations[imageRefAnnotationKey] = value
	if strings.HasPrefix(value, "sha256:") {
		template.ImageDescriptor.Digest = value
	}
}

func applyAgentBundleOverride(bundle *catalogv1.AgentBundle) {
	if bundle == nil {
		return
	}
	value := strings.TrimSpace(os.Getenv(agentBundleImageOverrideEnv[bundle.GetID()]))
	if value == "" {
		return
	}
	if bundle.ImageDescriptor == nil {
		bundle.ImageDescriptor = &catalogv1.OciImageDescriptor{}
	}
	if bundle.ImageDescriptor.Annotations == nil {
		bundle.ImageDescriptor.Annotations = map[string]string{}
	}
	bundle.ImageDescriptor.Annotations[imageRefAnnotationKey] = value
	if strings.HasPrefix(value, "sha256:") {
		bundle.ImageDescriptor.Digest = value
	}
}

func cloneRuntimeTemplateMap(in []*catalogv1.RuntimeTemplate) map[string]*catalogv1.RuntimeTemplate {
	out := make(map[string]*catalogv1.RuntimeTemplate, len(in))
	for _, template := range in {
		if template == nil {
			continue
		}
		id := strings.TrimSpace(template.GetID())
		if id == "" {
			continue
		}
		out[templateKey(id, template.GetVersion())] = proto.Clone(template).(*catalogv1.RuntimeTemplate)
	}
	return out
}

func cloneAgentBundleMap(in []*catalogv1.AgentBundle) map[string]*catalogv1.AgentBundle {
	out := make(map[string]*catalogv1.AgentBundle, len(in))
	for _, bundle := range in {
		if bundle == nil || strings.TrimSpace(bundle.GetID()) == "" {
			continue
		}
		out[templateKey(bundle.GetID(), bundle.GetVersion())] = proto.Clone(bundle).(*catalogv1.AgentBundle)
	}
	return out
}

func templateKey(id, version string) string {
	return strings.TrimSpace(id) + "\x00" + strings.TrimSpace(version)
}

func matchesFilter(template *catalogv1.RuntimeTemplate, filter *catalogv1.ListRuntimeTemplatesRequest) bool {
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
