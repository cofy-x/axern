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
	fmt.Fprintf(w, "Status: %s\n", EnvironmentStatusLabel(env.GetStatus()))
	if spec := env.GetSpec(); spec != nil {
		switch {
		case strings.TrimSpace(spec.GetTemplateID()) != "":
			fmt.Fprintln(w, "Source: template")
			fmt.Fprintf(w, "Template ID: %s\n", spec.GetTemplateID())
			if spec.GetTemplateVersion() != "" {
				fmt.Fprintf(w, "Template Version: %s\n", spec.GetTemplateVersion())
			}
		case spec.GetImage() != nil && strings.TrimSpace(spec.GetImage().GetRef()) != "":
			fmt.Fprintln(w, "Source: image")
			fmt.Fprintf(w, "Image Ref: %s\n", spec.GetImage().GetRef())
			if spec.GetImage().GetDigest() != "" {
				fmt.Fprintf(w, "Resolved Digest: %s\n", spec.GetImage().GetDigest())
			}
			if spec.GetImage().GetRegistryCredentialID() != "" {
				fmt.Fprintf(w, "Registry Credential ID: %s\n", spec.GetImage().GetRegistryCredentialID())
			}
			fmt.Fprintf(w, "Rootfs Readonly: %t\n", spec.GetImage().GetRootfsReadonly())
		}
	}
	if template := env.GetResolvedTemplate(); template != nil {
		if digest := template.GetImageDescriptor().GetDigest(); digest != "" {
			fmt.Fprintf(w, "Runtime Digest: %s\n", digest)
		}
		if ref := template.GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"]; ref != "" {
			fmt.Fprintf(w, "Normalized Image Ref: %s\n", ref)
		}
	}
	if message := env.GetMessage(); message != "" {
		fmt.Fprintf(w, "Message: %s\n", message)
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
			EnvironmentStatusLabel(env.GetStatus()),
			source,
			ref,
			digest,
		})
	}
	RenderTable(w, []string{"ID", "STATUS", "SOURCE", "REF", "DIGEST"}, rows)
}

func environmentSummary(env *environmentv1.Environment) (source, ref, digest string) {
	if env == nil {
		return "", "", ""
	}
	spec := env.GetSpec()
	switch {
	case strings.TrimSpace(spec.GetTemplateID()) != "":
		return "template", spec.GetTemplateID(), spec.GetTemplateVersion()
	case spec.GetImage() != nil && strings.TrimSpace(spec.GetImage().GetRef()) != "":
		return "image", spec.GetImage().GetRef(), spec.GetImage().GetDigest()
	default:
		return "unknown", "", ""
	}
}
