package langruntime

import (
	"fmt"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"google.golang.org/protobuf/proto"
)

func RootfsConfigFromRuntimeTemplate(fr *api.RuntimeTemplate) (RootfsConfig, error) {
	var cfg RootfsConfig
	if fr == nil || fr.Rootfs == nil {
		return cfg, fmt.Errorf("function runtime rootfs is nil")
	}

	switch fr.Rootfs.Type {
	case api.RootfsSrcType_S3:
		s3Config := fr.Rootfs.GetS3Config()
		if s3Config == nil {
			return cfg, fmt.Errorf("S3Config is nil while rootfs type is S3")
		}
		cfg = RootfsConfig{
			SrcType:         fr.Rootfs.Type,
			Endpoint:        s3Config.Endpoint,
			Bucket:          s3Config.Bucket,
			Object:          s3Config.Object,
			AccessKeyID:     s3Config.AccessKeyID,
			AccessKeySecret: s3Config.AccessKeySecret,
		}
	case api.RootfsSrcType_IMAGE:
		imageURL := fr.Rootfs.GetImageUrl()
		if imageURL == "" {
			return cfg, fmt.Errorf("Image URL is empty while rootfs type is IMAGE")
		}
		cfg = RootfsConfig{
			SrcType:  fr.Rootfs.Type,
			ImageUrl: imageURL,
		}
	case api.RootfsSrcType_LOCAL:
		path := fr.Rootfs.GetPath()
		if path == "" {
			return cfg, fmt.Errorf("Path empty while rootfs type is LOCAL")
		}
		cfg = RootfsConfig{
			SrcType: fr.Rootfs.Type,
			Path:    path,
		}
	default:
		return cfg, fmt.Errorf("Rootfs Type not supported: %v", fr.Rootfs.Type.String())
	}

	return cfg, nil
}

func rootfsTypeLabelFromConfig(cfg RootfsConfig) string {
	switch cfg.SrcType {
	case api.RootfsSrcType_LOCAL:
		return contract.StartupRootfsTypeLocal
	case api.RootfsSrcType_IMAGE:
		return contract.StartupRootfsTypeImage
	case api.RootfsSrcType_S3:
		return contract.StartupRootfsTypeS3
	default:
		return contract.StartupRootfsTypeUnknown
	}
}

func rootfsConfigMatchesRequest(current, requested RootfsConfig) bool {
	if requested.LeaseID == "" {
		requested.LeaseID = current.LeaseID
	}
	return current == requested
}

func rootfsConfigMessageFromRuntime(lr *LanguageRuntime) *api.RootfsConfig {
	if lr == nil || lr.RootFS == nil {
		return nil
	}

	cfg := lr.RootFS.Config()
	rootfsConfig := &api.RootfsConfig{
		Readonly: lr.Readonly,
		Type:     cfg.SrcType,
	}

	switch cfg.SrcType {
	case api.RootfsSrcType_S3:
		rootfsConfig.Source = &api.RootfsConfig_S3Config{
			S3Config: &api.S3Config{
				Endpoint:        cfg.Endpoint,
				Bucket:          cfg.Bucket,
				Object:          cfg.Object,
				AccessKeyID:     cfg.AccessKeyID,
				AccessKeySecret: cfg.AccessKeySecret,
			},
		}
	case api.RootfsSrcType_IMAGE:
		rootfsConfig.Source = &api.RootfsConfig_ImageUrl{
			ImageUrl: cfg.ImageUrl,
		}
	case api.RootfsSrcType_LOCAL:
		rootfsConfig.Source = &api.RootfsConfig_Path{
			Path: cfg.Path,
		}
	}

	return rootfsConfig
}

func languageRuntimeMatchesRuntimeTemplate(lr *LanguageRuntime, fr *api.RuntimeTemplate) bool {
	if lr == nil || fr == nil {
		return lr == nil && fr == nil
	}
	return proto.Equal(lr.RuntimeTemplate(), fr)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for k, v := range input {
		cloned[k] = v
	}
	return cloned
}

func cloneMounts(input []*api.Mount) []*api.Mount {
	if len(input) == 0 {
		return nil
	}
	cloned := make([]*api.Mount, 0, len(input))
	for _, mount := range input {
		if mount == nil {
			cloned = append(cloned, nil)
			continue
		}
		cloned = append(cloned, proto.Clone(mount).(*api.Mount))
	}
	return cloned
}
