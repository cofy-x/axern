package output

import (
	"fmt"
	"io"
	"strings"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
)

func RenderSecret(w io.Writer, secret *secretv1.Secret) {
	if secret == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", secret.GetID())
	fmt.Fprintf(w, "Namespace: %s\n", secret.GetNamespace())
	fmt.Fprintf(w, "Type: %s\n", SecretTypeLabel(secret.GetType()))
	if len(secret.GetDataKeys()) > 0 {
		fmt.Fprintf(w, "Data Keys: %s\n", strings.Join(secret.GetDataKeys(), ", "))
	}
	fmt.Fprintf(w, "Version: %d\n", secret.GetVersion())
}

func RenderSecretTable(w io.Writer, secrets []*secretv1.Secret) {
	rows := make([][]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret == nil {
			continue
		}
		rows = append(rows, []string{
			secret.GetID(),
			secret.GetNamespace(),
			SecretTypeLabel(secret.GetType()),
			strings.Join(secret.GetDataKeys(), ","),
			fmt.Sprintf("%d", secret.GetVersion()),
		})
	}
	RenderTable(w, []string{"ID", "NAMESPACE", "TYPE", "DATA_KEYS", "VERSION"}, rows)
}
