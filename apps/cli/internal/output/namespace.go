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
	fmt.Fprintf(w, "Created At: %s\n", FormatProtoTimestamp(namespace.GetCreatedAt()))
	if namespace.GetDeletedAt() != nil {
		fmt.Fprintf(w, "Deleted At: %s\n", FormatProtoTimestamp(namespace.GetDeletedAt()))
	}
}

func RenderNamespaceTable(w io.Writer, namespaces []*namespacev1.Namespace) {
	rows := make([][]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		if namespace == nil {
			continue
		}
		rows = append(rows, []string{
			namespace.GetNamespace(),
			FormatProtoTimestamp(namespace.GetCreatedAt()),
		})
	}
	RenderTable(w, []string{"NAMESPACE", "CREATED AT"}, rows)
}
