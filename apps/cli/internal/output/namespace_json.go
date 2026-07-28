package output

import (
	"io"

	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
)

type NamespaceResponseJSON struct {
	Namespace *NamespaceJSON `json:"namespace"`
}

type NamespaceListJSON struct {
	Namespaces []*NamespaceJSON `json:"namespaces"`
}

type NamespaceJSON struct {
	Namespace string `json:"namespace"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func PrintNamespaceJSON(w io.Writer, namespace *namespacev1.Namespace) error {
	return PrintJSON(w, NamespaceResponseJSON{Namespace: NewNamespaceJSON(namespace)})
}

func PrintNamespaceListJSON(w io.Writer, resp *namespacev1.ListNamespacesResponse) error {
	out := NamespaceListJSON{}
	if resp != nil {
		out.Namespaces = make([]*NamespaceJSON, 0, len(resp.GetNamespaces()))
		for _, namespace := range resp.GetNamespaces() {
			out.Namespaces = append(out.Namespaces, NewNamespaceJSON(namespace))
		}
	}
	return PrintJSON(w, out)
}

func NewNamespaceJSON(namespace *namespacev1.Namespace) *NamespaceJSON {
	if namespace == nil {
		return nil
	}
	return &NamespaceJSON{
		Namespace: namespace.GetNamespace(),
		Version:   namespace.GetVersion(),
		CreatedAt: FormatProtoTimestamp(namespace.GetCreatedAt()),
		UpdatedAt: FormatProtoTimestamp(namespace.GetUpdatedAt()),
	}
}
