package output

import (
	"fmt"
	"io"

	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
)

func RenderNamespace(w io.Writer, namespace *namespacev1.Namespace) {
	if namespace == nil {
		return
	}
	fmt.Fprintf(w, "Namespace: %s\n", namespace.GetNamespace())
	fmt.Fprintf(w, "Version: %d\n", namespace.GetVersion())
	fmt.Fprintf(w, "Created At: %s\n", FormatProtoTimestamp(namespace.GetCreatedAt()))
	fmt.Fprintf(w, "Updated At: %s\n", FormatProtoTimestamp(namespace.GetUpdatedAt()))
}

func RenderNamespaceTable(w io.Writer, namespaces []*namespacev1.Namespace) {
	rows := make([][]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		if namespace == nil {
			continue
		}
		rows = append(rows, []string{
			namespace.GetNamespace(),
			fmt.Sprintf("%d", namespace.GetVersion()),
			FormatProtoTimestamp(namespace.GetUpdatedAt()),
		})
	}
	RenderTable(w, []string{"NAMESPACE", "VERSION", "UPDATED AT"}, rows)
}
