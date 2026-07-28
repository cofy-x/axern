package output

import (
	"fmt"
	"io"
	"strings"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

func RenderRuntimeTemplateTable(w io.Writer, templates []*catalogv1.RuntimeTemplate) {
	rows := make([][]string, 0, len(templates))
	for _, template := range templates {
		if template == nil {
			continue
		}
		rows = append(rows, []string{
			template.GetID(),
			displayValue(template.GetVersion()),
			displayValue(template.GetLanguage()),
			displayValue(template.GetLanguageVersion()),
			displayValue(template.GetImageDescriptor().GetDigest()),
		})
	}
	RenderTable(w, []string{"ID", "VERSION", "LANGUAGE", "LANG_VERSION", "IMAGE"}, rows)
}

func RenderRuntimeTemplate(w io.Writer, template *catalogv1.RuntimeTemplate) {
	if template == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", template.GetID())
	fmt.Fprintf(w, "Version: %s\n", displayValue(template.GetVersion()))
	fmt.Fprintf(w, "Language: %s\n", displayValue(template.GetLanguage()))
	fmt.Fprintf(w, "Language Version: %s\n", displayValue(template.GetLanguageVersion()))
	fmt.Fprintf(w, "Image Digest: %s\n", displayValue(template.GetImageDescriptor().GetDigest()))
	if ref := template.GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"]; ref != "" {
		fmt.Fprintf(w, "Image Ref: %s\n", ref)
	}
	if len(template.GetImageDefaultArgv()) > 0 {
		fmt.Fprintf(w, "Image Default Argv: %s\n", strings.Join(template.GetImageDefaultArgv(), " "))
	}
	fmt.Fprintf(w, "Readonly Rootfs: %t\n", template.GetRootfsReadonly())
	if template.GetDescription() != "" {
		fmt.Fprintf(w, "Description: %s\n", template.GetDescription())
	}
}

func RenderAgentBundleTable(w io.Writer, bundles []*catalogv1.AgentBundle) {
	rows := make([][]string, 0, len(bundles))
	for _, bundle := range bundles {
		if bundle == nil {
			continue
		}
		rows = append(rows, []string{bundle.GetID(), displayValue(bundle.GetVersion()), displayValue(bundle.GetBinaryPath()), displayValue(bundle.GetImageDescriptor().GetDigest())})
	}
	RenderTable(w, []string{"ID", "VERSION", "BINARY", "IMAGE"}, rows)
}

func RenderAgentBundle(w io.Writer, bundle *catalogv1.AgentBundle) {
	if bundle == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", bundle.GetID())
	fmt.Fprintf(w, "Version: %s\n", displayValue(bundle.GetVersion()))
	fmt.Fprintf(w, "Binary Path: %s\n", displayValue(bundle.GetBinaryPath()))
	fmt.Fprintf(w, "Image Digest: %s\n", displayValue(bundle.GetImageDescriptor().GetDigest()))
	if ref := bundle.GetImageDescriptor().GetAnnotations()["org.opencontainers.image.ref.name"]; ref != "" {
		fmt.Fprintf(w, "Image Ref: %s\n", ref)
	}
	if bundle.GetDescription() != "" {
		fmt.Fprintf(w, "Description: %s\n", bundle.GetDescription())
	}
}

func displayValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
