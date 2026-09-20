package environmentcache

import (
	"fmt"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"google.golang.org/protobuf/proto"
)

func RootfsConfigFromResolvedEnvironment(fr *api.ResolvedEnvironment) (RootfsConfig, error) {
	var cfg RootfsConfig
	if fr == nil || fr.Rootfs == nil {
		return cfg, fmt.Errorf("runtime rootfs is nil")
	}

	switch fr.Rootfs.Type {
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

func rootfsConfigMessageFromRuntime(environment *PreparedEnvironment) *api.RootfsConfig {
	if environment == nil || environment.RootFS == nil {
		return nil
	}

	cfg := environment.RootFS.Config()
	rootfsConfig := &api.RootfsConfig{
		Readonly: environment.Readonly,
		Type:     cfg.SrcType,
	}

	switch cfg.SrcType {
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

func preparedEnvironmentMatchesResolvedSpec(environment *PreparedEnvironment, fr *api.ResolvedEnvironment) bool {
	if environment == nil || fr == nil {
		return environment == nil && fr == nil
	}
	return proto.Equal(environment.ResolvedEnvironment(), fr)
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
