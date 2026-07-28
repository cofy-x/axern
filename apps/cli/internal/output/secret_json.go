package output

import (
	"io"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
)

type SecretListJSON struct {
	Secrets    []*SecretJSON `json:"secrets"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type SecretResponseJSON struct {
	Secret *SecretJSON `json:"secret"`
}

type SecretJSON struct {
	ID        string            `json:"id"`
	Namespace string            `json:"namespace"`
	Type      string            `json:"type"`
	DataKeys  []string          `json:"data_keys,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Version   int64             `json:"version"`
	CreatedAt string            `json:"created_at,omitempty"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

func PrintSecretListJSON(w io.Writer, resp *secretv1.ListSecretsResponse) error {
	out := SecretListJSON{}
	if resp != nil {
		out.NextCursor = resp.GetNextCursor()
		out.Secrets = make([]*SecretJSON, 0, len(resp.GetSecrets()))
		for _, secret := range resp.GetSecrets() {
			out.Secrets = append(out.Secrets, NewSecretJSON(secret))
		}
	}
	return PrintJSON(w, out)
}

func PrintSecretResponseJSON(w io.Writer, secret *secretv1.Secret) error {
	return PrintJSON(w, SecretResponseJSON{Secret: NewSecretJSON(secret)})
}

func NewSecretJSON(secret *secretv1.Secret) *SecretJSON {
	if secret == nil {
		return nil
	}
	return &SecretJSON{
		ID:        secret.GetID(),
		Namespace: secret.GetNamespace(),
		Type:      SecretTypeLabel(secret.GetType()),
		DataKeys:  append([]string(nil), secret.GetDataKeys()...),
		Labels:    cloneStringMap(secret.GetLabels()),
		Version:   secret.GetVersion(),
		CreatedAt: FormatProtoTimestamp(secret.GetCreatedAt()),
		UpdatedAt: FormatProtoTimestamp(secret.GetUpdatedAt()),
	}
}
