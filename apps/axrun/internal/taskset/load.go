package taskset

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	maxDescriptorBytes      = 16 << 20
	maxDescriptorLayerBytes = maxDescriptorBytes + (1 << 20)
)

type Resolved struct {
	Reference        string
	DescriptorPath   string
	DescriptorDigest string
	Descriptor       Descriptor
	Tasks            []domain.TaskInstance
}

// Resolve loads a local TaskSet bundle or descriptor. OCI resolution is kept
// behind this boundary so plan/run and the HTTP service share one contract.
func Resolve(ref string) (Resolved, error) {
	return ResolveContext(context.Background(), ref)
}

func ResolveContext(ctx context.Context, ref string) (Resolved, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Resolved{}, fmt.Errorf("task set reference is required")
	}
	if strings.Contains(ref, "@sha256:") && !localExists(ref) {
		return resolveOCI(ctx, ref)
	}
	path, err := filepath.Abs(filepath.Clean(ref))
	if err != nil {
		return Resolved{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve task set %q: %w", ref, err)
	}
	bundleRoot := filepath.Dir(path)
	if info.IsDir() {
		bundleRoot = path
		path = filepath.Join(path, "descriptor.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Resolved{}, fmt.Errorf("read task set descriptor: %w", err)
	}
	var descriptor Descriptor
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&descriptor); err != nil {
		return Resolved{}, fmt.Errorf("parse task set descriptor: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Resolved{}, fmt.Errorf("parse task set descriptor: %w", err)
	}
	if err := validateDescriptor(descriptor); err != nil {
		return Resolved{}, err
	}
	if err := verifyLocalBundle(bundleRoot, descriptor); err != nil {
		return Resolved{}, err
	}
	hash := sha256.Sum256(data)
	resolved := Resolved{
		Reference:        ref,
		DescriptorPath:   path,
		DescriptorDigest: "sha256:" + hex.EncodeToString(hash[:]),
		Descriptor:       descriptor,
	}
	resolved.Tasks = resolveDescriptorTasks(descriptor, bundleRoot)
	return resolved, nil
}

func resolveOCI(ctx context.Context, ref string) (Resolved, error) {
	parsed, err := name.ParseReference(ref, name.WeakValidation)
	if err != nil {
		return Resolved{}, err
	}
	descriptor, err := remote.Get(parsed, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve OCI TaskSet %q: %w", ref, err)
	}
	image, err := descriptor.Image()
	if err != nil {
		return Resolved{}, fmt.Errorf("TaskSet descriptor artifact is not an OCI image: %w", err)
	}
	if !supportedDescriptorMediaType(descriptor.Descriptor.MediaType) {
		return Resolved{}, fmt.Errorf("TaskSet descriptor artifact has unsupported manifest media type %q", descriptor.Descriptor.MediaType)
	}
	layers, err := image.Layers()
	if err != nil || len(layers) != 1 {
		return Resolved{}, fmt.Errorf("TaskSet descriptor artifact must contain exactly one layer")
	}
	layerMediaType, err := layers[0].MediaType()
	if err != nil {
		return Resolved{}, fmt.Errorf("read TaskSet descriptor layer media type: %w", err)
	}
	if !supportedDescriptorLayerMediaType(layerMediaType) {
		return Resolved{}, fmt.Errorf("TaskSet descriptor layer has unsupported media type %q", layerMediaType)
	}
	layerSize, err := layers[0].Size()
	if err != nil {
		return Resolved{}, fmt.Errorf("read TaskSet descriptor layer size: %w", err)
	}
	if layerSize <= 0 || layerSize > maxDescriptorLayerBytes {
		return Resolved{}, fmt.Errorf("TaskSet descriptor layer size must be between 1 byte and 17 MiB")
	}
	reader, err := layers[0].Uncompressed()
	if err != nil {
		return Resolved{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxDescriptorLayerBytes+1))
	if err != nil {
		return Resolved{}, err
	}
	if len(data) == 0 || len(data) > maxDescriptorLayerBytes {
		return Resolved{}, fmt.Errorf("TaskSet descriptor layer exceeds the 17 MiB transport limit")
	}
	data, err = decodeDescriptorLayer(layerMediaType, data)
	if err != nil {
		return Resolved{}, err
	}
	var taskSet Descriptor
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&taskSet); err != nil {
		return Resolved{}, fmt.Errorf("parse TaskSet descriptor: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Resolved{}, fmt.Errorf("parse TaskSet descriptor: %w", err)
	}
	if err := validateDescriptor(taskSet); err != nil {
		return Resolved{}, err
	}
	if len(taskSet.Payloads) == 0 {
		return Resolved{}, fmt.Errorf("published TaskSet descriptor contains no payload variants")
	}
	for _, payload := range taskSet.Payloads {
		payloadRef, parseErr := name.ParseReference(payload.Reference, name.WeakValidation)
		if parseErr != nil {
			return Resolved{}, fmt.Errorf("parse %s payload reference: %w", payload.Format, parseErr)
		}
		resolvedPayload, headErr := remote.Head(payloadRef, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if headErr != nil {
			return Resolved{}, fmt.Errorf("resolve %s payload: %w", payload.Format, headErr)
		}
		if resolvedPayload.Digest.String() != payload.Digest {
			return Resolved{}, fmt.Errorf("%s payload digest mismatch: descriptor has %s, registry has %s", payload.Format, payload.Digest, resolvedPayload.Digest)
		}
		if !supportedPayloadMediaType(resolvedPayload.MediaType) {
			return Resolved{}, fmt.Errorf("%s payload has unsupported media type %q", payload.Format, resolvedPayload.MediaType)
		}
		if payload.MediaType != "" && payload.MediaType != string(resolvedPayload.MediaType) {
			return Resolved{}, fmt.Errorf("%s payload media type mismatch: descriptor has %s, registry has %s", payload.Format, payload.MediaType, resolvedPayload.MediaType)
		}
		if payload.SizeBytes > 0 && payload.SizeBytes != resolvedPayload.Size {
			return Resolved{}, fmt.Errorf("%s payload size mismatch: descriptor has %d, registry has %d", payload.Format, payload.SizeBytes, resolvedPayload.Size)
		}
	}
	resolved := Resolved{Reference: ref, DescriptorDigest: descriptor.Descriptor.Digest.String(), Descriptor: taskSet}
	resolved.Tasks = resolveDescriptorTasks(taskSet, "")
	return resolved, nil
}

// Registries may preserve an OCI image manifest or normalize the transport
// envelope to Docker schema 2. The TaskSet contract is enforced by the typed,
// single descriptor layer below, so accepting both image envelopes keeps
// resolution portable without accepting indexes, manifest lists, or ordinary
// container images as TaskSet descriptors.
func supportedDescriptorMediaType(mediaType types.MediaType) bool {
	return mediaType == types.OCIManifestSchema1 || mediaType == types.DockerManifestSchema2
}

// Some registries normalize every layer in a Docker schema 2 manifest to the
// Docker rootfs layer media type, even when the pushed OCI artifact used the
// Axrun-specific descriptor media type. Accept that known transport rewrite;
// semantic identity is still established by the single-layer invariant, the
// size bound, strict JSON decoding, and the axrun/v1 TaskSet contract.
func supportedDescriptorLayerMediaType(mediaType types.MediaType) bool {
	return mediaType == types.MediaType(DescriptorMediaType) || mediaType == types.DockerLayer
}

func decodeDescriptorLayer(mediaType types.MediaType, data []byte) ([]byte, error) {
	if mediaType == types.MediaType(DescriptorMediaType) {
		if len(data) == 0 || len(data) > maxDescriptorBytes {
			return nil, fmt.Errorf("TaskSet descriptor layer must contain 1 byte to 16 MiB of JSON")
		}
		return data, nil
	}

	archive := tar.NewReader(bytes.NewReader(data))
	header, err := archive.Next()
	if err != nil {
		return nil, fmt.Errorf("read registry-normalized TaskSet descriptor layer: %w", err)
	}
	if header.Name != "descriptor.json" || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) {
		return nil, fmt.Errorf("registry-normalized TaskSet descriptor layer must contain only regular file descriptor.json")
	}
	if header.Size <= 0 || header.Size > maxDescriptorBytes {
		return nil, fmt.Errorf("TaskSet descriptor must contain 1 byte to 16 MiB of JSON")
	}
	descriptor, err := io.ReadAll(io.LimitReader(archive, maxDescriptorBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read registry-normalized TaskSet descriptor: %w", err)
	}
	if int64(len(descriptor)) != header.Size {
		return nil, fmt.Errorf("registry-normalized TaskSet descriptor size mismatch")
	}
	if _, err := archive.Next(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("registry-normalized TaskSet descriptor layer contains unexpected additional entries")
		}
		return nil, fmt.Errorf("finish registry-normalized TaskSet descriptor layer: %w", err)
	}
	return descriptor, nil
}

func supportedPayloadMediaType(mediaType types.MediaType) bool {
	switch mediaType {
	case types.OCIManifestSchema1, types.OCIImageIndex, types.DockerManifestSchema2, types.DockerManifestList:
		return true
	default:
		return false
	}
}

func resolveDescriptorTasks(descriptor Descriptor, bundleRoot string) []domain.TaskInstance {
	instances := make([]domain.TaskInstance, 0, len(descriptor.Tasks))
	for _, task := range descriptor.Tasks {
		instance := cloneTaskInstanceForResolution(task.Instance)
		if instance.InitialState == nil {
			instance.InitialState = &domain.InitialStateSpec{}
		}
		if len(descriptor.Payloads) > 0 {
			instance.InitialState.Type = "taskset_workspace_image"
			instance.InitialState.Path = ""
			instance.InitialState.WorkspaceImage = &domain.WorkspaceImageSourceSpec{
				SourcePath: task.WorkspaceSubpath,
				Target:     instance.Sandbox.Workdir,
			}
			for _, payload := range orderedPayloads(descriptor.Payloads) {
				instance.InitialState.WorkspaceImage.Variants = append(
					instance.InitialState.WorkspaceImage.Variants,
					domain.WorkspaceImageVariantSpec{
						Format: payload.Format,
						Image:  payload.Reference,
					},
				)
			}
		} else {
			instance.InitialState.Type = "directory"
			instance.InitialState.Path = filepath.Join(bundleRoot, "payload", filepath.FromSlash(task.WorkspaceSubpath))
			for index := range instance.Verifier.Assets {
				instance.Verifier.Assets[index].Path = filepath.Join(bundleRoot, "payload", filepath.FromSlash(instance.Verifier.Assets[index].Path))
			}
			if instance.Oracle != nil && instance.Oracle.Path != "" {
				instance.Oracle.Path = filepath.Join(bundleRoot, "payload", filepath.FromSlash(instance.Oracle.Path))
			}
		}
		instances = append(instances, instance)
	}
	return instances
}

// Resolution rewrites local paths and workspace delivery without changing the
// canonical descriptor. Several TaskInstance fields contain pointers or slices;
// a shallow copy would leak those runtime rewrites into a subsequently
// published descriptor.
func cloneTaskInstanceForResolution(in domain.TaskInstance) domain.TaskInstance {
	out := in
	out.Verifier.Assets = append([]domain.VerifierAssetSpec(nil), in.Verifier.Assets...)
	if in.InitialState != nil {
		initial := *in.InitialState
		initial.Files = append([]string(nil), in.InitialState.Files...)
		initial.ExcludePaths = append([]string(nil), in.InitialState.ExcludePaths...)
		if in.InitialState.WorkspaceImage != nil {
			workspace := *in.InitialState.WorkspaceImage
			workspace.Variants = append([]domain.WorkspaceImageVariantSpec(nil), in.InitialState.WorkspaceImage.Variants...)
			initial.WorkspaceImage = &workspace
		}
		out.InitialState = &initial
	}
	if in.Oracle != nil {
		oracle := *in.Oracle
		out.Oracle = &oracle
	}
	return out
}

func orderedPayloads(payloads []PayloadDescriptor) []PayloadDescriptor {
	ordered := append([]PayloadDescriptor(nil), payloads...)
	sort.SliceStable(ordered, func(i, j int) bool {
		priority := func(format string) int {
			if format == "nydus" {
				return 0
			}
			return 1
		}
		return priority(ordered[i].Format) < priority(ordered[j].Format)
	})
	return ordered
}

func validateDescriptor(descriptor Descriptor) error {
	if descriptor.APIVersion != APIVersion || descriptor.Kind != DescriptorKind {
		return fmt.Errorf("task set descriptor requires api_version %q and kind %s", APIVersion, DescriptorKind)
	}
	if strings.TrimSpace(descriptor.Name) == "" {
		return fmt.Errorf("task set descriptor name is required")
	}
	if strings.TrimSpace(descriptor.Provenance.Compiler) == "" || descriptor.Provenance.Contract != APIVersion || strings.TrimSpace(descriptor.Provenance.PackerVersion) == "" {
		return fmt.Errorf("task set descriptor provenance must identify a compiler, %s contract, and packer version", APIVersion)
	}
	if !strings.HasPrefix(descriptor.SourceDigest, "sha256:") {
		return fmt.Errorf("task set descriptor source_digest must use sha256")
	}
	if _, err := v1.NewHash(descriptor.SourceDigest); err != nil {
		return fmt.Errorf("task set descriptor source_digest is invalid: %w", err)
	}
	if len(descriptor.Tasks) == 0 {
		return fmt.Errorf("task set descriptor contains no tasks")
	}
	seenFormats := map[string]bool{}
	for _, payload := range descriptor.Payloads {
		if payload.Format != "nydus" && payload.Format != "oci" {
			return fmt.Errorf("unsupported payload format %q", payload.Format)
		}
		if seenFormats[payload.Format] {
			return fmt.Errorf("payload format %q is duplicated", payload.Format)
		}
		seenFormats[payload.Format] = true
		if !strings.HasPrefix(payload.Digest, "sha256:") {
			return fmt.Errorf("payload %q digest must use sha256", payload.Format)
		}
		if _, err := v1.NewHash(payload.Digest); err != nil {
			return fmt.Errorf("payload %q digest is invalid: %w", payload.Format, err)
		}
		if !strings.Contains(payload.Reference, "@"+payload.Digest) {
			return fmt.Errorf("payload %q must use an immutable digest reference", payload.Format)
		}
	}
	seen := map[string]bool{}
	for _, task := range descriptor.Tasks {
		if err := contract.ValidatePathSegment("task id", task.Instance.ID); err != nil {
			return err
		}
		if seen[task.Instance.ID] {
			return fmt.Errorf("task id %q is duplicated", task.Instance.ID)
		}
		seen[task.Instance.ID] = true
		if err := validateTaskTemplate(TaskTemplate{
			Sandbox:      task.Instance.Sandbox,
			Verifier:     task.Instance.Verifier,
			Outputs:      task.Instance.Outputs,
			Resources:    task.Instance.Resources,
			Timeouts:     task.Instance.Timeouts,
			Oracle:       task.Instance.Oracle,
			Tags:         task.Instance.Tags,
			Capabilities: task.Instance.Capabilities,
		}); err != nil {
			return fmt.Errorf("task %q contract: %w", task.Instance.ID, err)
		}
		if err := validatePayloadSubpath(task.WorkspaceSubpath); err != nil {
			return fmt.Errorf("task %q workspace: %w", task.Instance.ID, err)
		}
		workspacePrefix := filepath.ToSlash(filepath.Join("tasks", task.Instance.ID, "workspace"))
		verifierPrefix := filepath.ToSlash(filepath.Join("tasks", task.Instance.ID, "verifier"))
		oraclePrefix := filepath.ToSlash(filepath.Join("tasks", task.Instance.ID, "oracle"))
		if task.WorkspaceSubpath != workspacePrefix {
			return fmt.Errorf("task %q workspace subpath must be %q", task.Instance.ID, workspacePrefix)
		}
		if task.VerifierSubpath != "" && task.VerifierSubpath != verifierPrefix {
			return fmt.Errorf("task %q verifier subpath must be %q", task.Instance.ID, verifierPrefix)
		}
		if task.OracleSubpath != "" && task.OracleSubpath != oraclePrefix {
			return fmt.Errorf("task %q oracle subpath must be %q", task.Instance.ID, oraclePrefix)
		}
		for _, asset := range task.Instance.Verifier.Assets {
			if !pathWithinPrefix(asset.Path, verifierPrefix) {
				return fmt.Errorf("task %q verifier asset %q is outside %q", task.Instance.ID, asset.Path, verifierPrefix)
			}
		}
		if task.Instance.Oracle != nil && task.Instance.Oracle.Path != "" && !pathWithinPrefix(task.Instance.Oracle.Path, oraclePrefix) {
			return fmt.Errorf("task %q oracle asset %q is outside %q", task.Instance.ID, task.Instance.Oracle.Path, oraclePrefix)
		}
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func verifyLocalBundle(bundleRoot string, descriptor Descriptor) error {
	if len(descriptor.Payloads) != 0 {
		return fmt.Errorf("published TaskSet descriptors must be resolved by immutable OCI digest")
	}
	payloadPath := filepath.Join(bundleRoot, "payload.tar")
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		return fmt.Errorf("read local TaskSet payload: %w", err)
	}
	repacked, err := deterministicTar(filepath.Join(bundleRoot, "payload"))
	if err != nil {
		return fmt.Errorf("verify local TaskSet payload: %w", err)
	}
	if !bytes.Equal(payload, repacked) {
		return fmt.Errorf("local TaskSet payload directory does not match payload.tar")
	}
	payloadDigest := sha256.Sum256(payload)
	want, err := computeSourceDigest(descriptor, payloadDigest)
	if err != nil {
		return err
	}
	if descriptor.SourceDigest != want {
		return fmt.Errorf("local TaskSet source digest mismatch: descriptor has %s, computed %s", descriptor.SourceDigest, want)
	}
	return nil
}

func pathWithinPrefix(value, prefix string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	return clean == prefix || strings.HasPrefix(clean, prefix+"/")
}

func validatePayloadSubpath(value string) error {
	clean := filepath.Clean(filepath.FromSlash(value))
	if value == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid payload subpath %q", value)
	}
	return nil
}

func localExists(ref string) bool {
	_, err := os.Stat(ref)
	return err == nil
}
