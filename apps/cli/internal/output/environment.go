package output

import (
	"fmt"
	"io"
	"strings"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

func RenderEnvironment(w io.Writer, env *environmentv1.Environment) {
	if env == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", env.GetID())
	fmt.Fprintf(w, "Namespace: %s\n", env.GetNamespace())
	if spec := env.GetSpec(); spec != nil {
		if spec.GetImage() != nil && strings.TrimSpace(spec.GetImage().GetRef()) != "" {
			fmt.Fprintln(w, "Source: image")
			fmt.Fprintf(w, "Image Ref: %s\n", spec.GetImage().GetRef())
			if spec.GetImage().GetRegistryCredentialID() != "" {
				fmt.Fprintf(w, "Registry Credential ID: %s\n", spec.GetImage().GetRegistryCredentialID())
			}
			fmt.Fprintf(w, "Rootfs Readonly: %t\n", spec.GetImage().GetRootfsReadonly())
		}
	}
	if resolved := env.GetResolvedSpec(); resolved != nil {
		if digest := resolved.GetImageDescriptor().GetDigest(); digest != "" {
			fmt.Fprintf(w, "Resolved Digest: %s\n", digest)
		}
		if ref := resolved.GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"]; ref != "" {
			fmt.Fprintf(w, "Normalized Image Ref: %s\n", ref)
		}
	}
}

func RenderEnvironmentTable(w io.Writer, envs []*environmentv1.Environment) {
	rows := make([][]string, 0, len(envs))
	for _, env := range envs {
		if env == nil {
			continue
		}
		source, ref, digest := environmentSummary(env)
		rows = append(rows, []string{
			env.GetID(),
			source,
			ref,
			digest,
		})
	}
	RenderTable(w, []string{"ID", "SOURCE", "REF", "DIGEST"}, rows)
}

func environmentSummary(env *environmentv1.Environment) (source, ref, digest string) {
	if env == nil {
		return "", "", ""
	}
	spec := env.GetSpec()
	if spec.GetImage() != nil && strings.TrimSpace(spec.GetImage().GetRef()) != "" {
		return "image", spec.GetImage().GetRef(), env.GetResolvedSpec().GetImageDescriptor().GetDigest()
	}
	return "unknown", "", ""
}
