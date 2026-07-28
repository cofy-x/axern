package pgsecret

import secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"

func matchFilter(secret *secretv1.Secret, filter *secretv1.SecretListFilter) bool {
	if secret == nil || filter == nil {
		return true
	}
	if ns := normalizeNamespace(filter.GetNamespace()); ns != "" && secret.GetNamespace() != ns {
		return false
	}
	if filter.GetType() != secretv1.SecretType_SECRET_TYPE_UNSPECIFIED && filter.GetType() != secret.GetType() {
		return false
	}
	for key, value := range filter.GetLabels() {
		if secret.GetLabels()[key] != value {
			return false
		}
	}
	return true
}
