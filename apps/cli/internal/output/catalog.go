package output

import (
	"fmt"
	"io"
	"strings"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

func RenderEnvironmentTemplateTable(w io.Writer, templates []*catalogv1.EnvironmentTemplate) {
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

func RenderEnvironmentTemplate(w io.Writer, template *catalogv1.EnvironmentTemplate) {
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

func displayValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
