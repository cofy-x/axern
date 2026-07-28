package imageregistry

import (
	"strings"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/registryauth"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

type registryKeychain struct {
	auths registryauth.Config
}

func (k *registryKeychain) Resolve(res authn.Resource) (authn.Authenticator, error) {
	host := res.RegistryStr()
	candidates := []string{host}
	if host == name.DefaultRegistry {
		candidates = append(candidates, "docker.io")
	}

	for _, candidate := range candidates {
		if entry, ok := k.auths[candidate]; ok && entry.Auth != "" {
			return authn.FromConfig(authn.AuthConfig{Auth: entry.Auth}), nil
		}
		for key, entry := range k.auths {
			if strings.HasPrefix(key, candidate+"/") && entry.Auth != "" {
				return authn.FromConfig(authn.AuthConfig{Auth: entry.Auth}), nil
			}
		}
	}

	return authn.Anonymous, nil
}
