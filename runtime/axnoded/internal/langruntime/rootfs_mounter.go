package langruntime

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	runtime_api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
)

// ImageMounter abstracts rootfs mount/umount operations for testability.
type ImageMounter interface {
	Resolve(cfg RootfsConfig) (RootfsConfig, error)
	Mount(cfg RootfsConfig) (*MountResult, error)
	Umount(cfg RootfsConfig) error
	Reconcile(leaseIDs []string) error
}

type MountResult struct {
	Path           string
	Env            []string
	ImageConfig    *ImageConfig
	ImmutableMount *runtime_api.ImmutableRootfsMount
}

type imageManagerClient interface {
	ResolveOCIImageCacheKey(imageURL string) (string, error)
	MountOCI(req *ociMountRequest) (*imageManagerMountInfo, error)
	UmountOCI(req *ociUmountRequest) error
	MountOSS(req *ossMountRequest) (*imageManagerMountInfo, error)
	UmountOSS(req *ossUmountRequest) error
	ReconcileMountLeases(req *reconcileMountLeasesRequest) error
}

func (d *defaultMounter) Reconcile(leaseIDs []string) error {
	if d.client == nil {
		return nil
	}
	leaseIDs = append([]string(nil), leaseIDs...)
	sort.Strings(leaseIDs)
	return d.client.ReconcileMountLeases(&reconcileMountLeasesRequest{Owner: "axnoded", LeaseIDs: leaseIDs})
}

func (d *defaultMounter) Resolve(cfg RootfsConfig) (RootfsConfig, error) {
	if cfg.SrcType == runtime_api.RootfsSrcType_IMAGE && cfg.ImageCacheKey == "" {
		if d.client == nil {
			return cfg, fmt.Errorf("image manager client is not configured")
		}
		cacheKey, err := d.client.ResolveOCIImageCacheKey(cfg.ImageUrl)
		if err != nil {
			return cfg, err
		}
		cfg.ImageCacheKey = cacheKey
	}
	if cfg.SrcType == runtime_api.RootfsSrcType_IMAGE || cfg.SrcType == runtime_api.RootfsSrcType_S3 {
		cfg.LeaseID = rootfsLeaseID(cfg)
	}
	return cfg, nil
}

func rootfsLeaseID(cfg RootfsConfig) string {
	identity := struct {
		SourceType    runtime_api.RootfsSrcType `json:"source_type"`
		ImageURL      string                    `json:"image_url,omitempty"`
		ImageCacheKey string                    `json:"image_cache_key,omitempty"`
		Endpoint      string                    `json:"endpoint,omitempty"`
		Bucket        string                    `json:"bucket,omitempty"`
		Object        string                    `json:"object,omitempty"`
		Credential    string                    `json:"credential_fingerprint,omitempty"`
	}{SourceType: cfg.SrcType}
	switch cfg.SrcType {
	case runtime_api.RootfsSrcType_IMAGE:
		identity.ImageURL = cfg.ImageUrl
		identity.ImageCacheKey = cfg.ImageCacheKey
		identity.Credential = credentialFingerprint(cfg.DockerConfigJSON)
	case runtime_api.RootfsSrcType_S3:
		identity.Endpoint = cfg.Endpoint
		identity.Bucket = cfg.Bucket
		identity.Object = cfg.Object
		identity.Credential = credentialFingerprint(cfg.AccessKeyID + "\x00" + cfg.AccessKeySecret)
	}
	data, err := json.Marshal(identity)
	if err != nil {
		panic(fmt.Sprintf("marshal rootfs lease identity: %v", err))
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("axnoded-rootfs:%x", sum[:])
}

func credentialFingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum[:])
}

type defaultMounter struct {
	client imageManagerClient
}

func NewDefaultMounter(enabled bool, socketPath string) ImageMounter {
	return &defaultMounter{
		client: newImageManagerClient(enabled, socketPath),
	}
}

func (rf *RootFS) MountImage() error {
	if rf.path != "" {
		return fmt.Errorf("already mounted")
	}
	result, err := rf.mounter.Mount(rf.cfg)
	if err != nil {
		return err
	}
	if err := validateMountResult(result, rf.cfg.LeaseID); err != nil {
		cleanupErr := rf.mounter.Umount(rf.cfg)
		return errors.Join(err, cleanupErr)
	}
	rf.path = result.Path
	rf.env = append([]string(nil), result.Env...)
	rf.imageConfig = cloneImageConfig(result.ImageConfig)
	rf.immutableMount = cloneImmutableMount(result.ImmutableMount)
	return nil
}

func validateMountResult(result *MountResult, leaseID string) error {
	if result == nil || result.Path == "" {
		return fmt.Errorf("mount result path is empty")
	}
	descriptor := rootfsview.ImmutableMountFromProto(result.ImmutableMount)
	if descriptor.LeaseID != leaseID {
		return fmt.Errorf("mount result lease %q differs from requested lease", descriptor.LeaseID)
	}
	if err := rootfsview.ValidateImmutableMountDescriptorContract(descriptor, result.Path); err != nil {
		return fmt.Errorf("mount result immutable rootfs contract is invalid: %w", err)
	}
	return nil
}

func (rf *RootFS) UmountImage() error {
	return rf.mounter.Umount(rf.cfg)
}

func (d *defaultMounter) Mount(cfg RootfsConfig) (*MountResult, error) {
	switch cfg.SrcType {
	case runtime_api.RootfsSrcType_LOCAL:
		mount, err := DescribeLocalRootfs(cfg.Path)
		if err != nil {
			return nil, err
		}
		return &MountResult{Path: cfg.Path, ImmutableMount: mount}, nil
	case runtime_api.RootfsSrcType_IMAGE:
		if d.client == nil {
			return nil, fmt.Errorf("image manager client is not configured")
		}
		info, err := d.client.MountOCI(&ociMountRequest{
			ImageURL:         cfg.ImageUrl,
			CacheKey:         cfg.ImageCacheKey,
			DockerConfigJSON: cfg.DockerConfigJSON,
			LeaseID:          cfg.LeaseID,
			Owner:            "axnoded",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to mount image rootfs %s: %w", cfg.ImageUrl, err)
		}
		return mountResultFromImageManager(info), nil
	case runtime_api.RootfsSrcType_S3:
		if d.client == nil {
			return nil, fmt.Errorf("image manager client is not configured")
		}
		info, err := d.client.MountOSS(&ossMountRequest{
			Endpoint:        cfg.Endpoint,
			Bucket:          cfg.Bucket,
			Object:          cfg.Object,
			AccessKeyID:     cfg.AccessKeyID,
			AccessKeySecret: cfg.AccessKeySecret,
			LeaseID:         cfg.LeaseID,
			Owner:           "axnoded",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to mount oss rootfs %s/%s: %w", cfg.Bucket, cfg.Object, err)
		}
		return mountResultFromImageManager(info), nil
	default:
		return nil, fmt.Errorf("unsupported rootfs type: %v", cfg.SrcType.String())
	}
}

func DescribeLocalRootfs(path string) (*runtime_api.ImmutableRootfsMount, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("failed to stat local rootfs path %s: %w", path, err)
	}
	facts, err := rootfsview.InspectBacking(path)
	if err != nil {
		return nil, fmt.Errorf("inspect local rootfs immutable mount: %w", err)
	}
	return immutableMountProto(facts.ImmutableMountDescriptor("")), nil
}

func mountResultFromImageManager(info *imageManagerMountInfo) *MountResult {
	if info == nil {
		return nil
	}
	return &MountResult{
		Path:           info.MountPath,
		Env:            append([]string(nil), info.Env...),
		ImageConfig:    cloneImageConfig(info.ImageConfig),
		ImmutableMount: imageManagerImmutableMountProto(info.ImmutableMount),
	}
}

func imageManagerImmutableMountProto(in *imageManagerImmutableMount) *runtime_api.ImmutableRootfsMount {
	if in == nil {
		return nil
	}
	return &runtime_api.ImmutableRootfsMount{
		Identity: in.Identity, EffectiveRoot: in.EffectiveRoot, Filesystem: in.Filesystem,
		BackingFilesystems: append([]string(nil), in.BackingFilesystems...), LowerDirs: append([]string(nil), in.LowerDirs...),
		Readonly: in.Readonly, LeaseID: in.LeaseID,
	}
}

func immutableMountProto(in rootfsview.ImmutableMountDescriptor) *runtime_api.ImmutableRootfsMount {
	return &runtime_api.ImmutableRootfsMount{
		Identity: in.Identity, EffectiveRoot: in.EffectiveRoot, Filesystem: in.Filesystem,
		BackingFilesystems: append([]string(nil), in.BackingFilesystems...), LowerDirs: append([]string(nil), in.LowerDirs...),
		Readonly: in.Readonly, LeaseID: in.LeaseID,
	}
}

func cloneImmutableMount(in *runtime_api.ImmutableRootfsMount) *runtime_api.ImmutableRootfsMount {
	if in == nil {
		return nil
	}
	return &runtime_api.ImmutableRootfsMount{
		Identity: in.GetIdentity(), EffectiveRoot: in.GetEffectiveRoot(), Filesystem: in.GetFilesystem(),
		BackingFilesystems: append([]string(nil), in.GetBackingFilesystems()...), LowerDirs: append([]string(nil), in.GetLowerDirs()...),
		Readonly: in.GetReadonly(), LeaseID: in.GetLeaseID(),
	}
}

func cloneImageConfig(in *ImageConfig) *ImageConfig {
	if in == nil {
		return nil
	}
	return &ImageConfig{
		Entrypoint: append([]string(nil), in.Entrypoint...),
		Cmd:        append([]string(nil), in.Cmd...),
		WorkingDir: in.WorkingDir,
		User:       in.User,
	}
}

func (d *defaultMounter) Umount(cfg RootfsConfig) error {
	switch cfg.SrcType {
	case runtime_api.RootfsSrcType_LOCAL:
		return nil
	case runtime_api.RootfsSrcType_IMAGE:
		if d.client == nil {
			return fmt.Errorf("image manager client is not configured")
		}
		return d.client.UmountOCI(&ociUmountRequest{ImageURL: cfg.ImageUrl, CacheKey: cfg.ImageCacheKey, LeaseID: cfg.LeaseID})
	case runtime_api.RootfsSrcType_S3:
		if d.client == nil {
			return fmt.Errorf("image manager client is not configured")
		}
		return d.client.UmountOSS(&ossUmountRequest{
			Endpoint: cfg.Endpoint,
			Bucket:   cfg.Bucket,
			Object:   cfg.Object,
			LeaseID:  cfg.LeaseID,
		})
	default:
		return fmt.Errorf("unsupported rootfs type: %v", cfg.SrcType.String())
	}
}
