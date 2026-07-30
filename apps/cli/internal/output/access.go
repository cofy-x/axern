package output

import (
	"io"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
)

type PrincipalJSON struct {
	PrincipalID string `json:"principal_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type PrincipalCredentialJSON struct {
	CredentialID        string `json:"credential_id"`
	PrincipalID         string `json:"principal_id"`
	Fingerprint         string `json:"fingerprint"`
	CertificateNotAfter string `json:"certificate_not_after"`
	Label               string `json:"label"`
	CreatedAt           string `json:"created_at"`
	RevokedAt           string `json:"revoked_at,omitempty"`
}

type RoleBindingJSON struct {
	BindingID            string `json:"binding_id"`
	PrincipalID          string `json:"principal_id"`
	ScopeType            string `json:"scope_type"`
	Namespace            string `json:"namespace,omitempty"`
	Role                 string `json:"role"`
	CreatedByPrincipalID string `json:"created_by_principal_id,omitempty"`
	CreatedAt            string `json:"created_at"`
	RevokedByPrincipalID string `json:"revoked_by_principal_id,omitempty"`
	RevokedAt            string `json:"revoked_at,omitempty"`
}

func PrintPrincipalJSON(w io.Writer, principal *adminv1.Principal) error {
	return PrintJSON(w, principalJSON(principal))
}

func PrintPrincipalListJSON(w io.Writer, principals []*adminv1.Principal) error {
	items := make([]*PrincipalJSON, 0, len(principals))
	for _, principal := range principals {
		items = append(items, principalJSON(principal))
	}
	return PrintJSON(w, struct {
		Principals []*PrincipalJSON `json:"principals"`
	}{Principals: items})
}

func PrintPrincipalCredentialJSON(w io.Writer, credential *adminv1.PrincipalCredential) error {
	return PrintJSON(w, credentialJSON(credential))
}

func PrintPrincipalCredentialListJSON(w io.Writer, credentials []*adminv1.PrincipalCredential) error {
	items := make([]*PrincipalCredentialJSON, 0, len(credentials))
	for _, credential := range credentials {
		items = append(items, credentialJSON(credential))
	}
	return PrintJSON(w, struct {
		Credentials []*PrincipalCredentialJSON `json:"credentials"`
	}{Credentials: items})
}

func PrintRoleBindingJSON(w io.Writer, binding *adminv1.RoleBinding) error {
	return PrintJSON(w, bindingJSON(binding))
}

func PrintRoleBindingListJSON(w io.Writer, bindings []*adminv1.RoleBinding) error {
	items := make([]*RoleBindingJSON, 0, len(bindings))
	for _, binding := range bindings {
		items = append(items, bindingJSON(binding))
	}
	return PrintJSON(w, struct {
		Bindings []*RoleBindingJSON `json:"bindings"`
	}{Bindings: items})
}

func PrintIdentityJSON(w io.Writer, response *identityv1.WhoAmIResponse) error {
	type roleJSON struct {
		Role      string `json:"role"`
		ScopeType string `json:"scope_type"`
		Namespace string `json:"namespace,omitempty"`
	}
	type principalIdentityJSON struct {
		PrincipalID string `json:"principal_id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Kind        string `json:"kind"`
	}
	type credentialIdentityJSON struct {
		CredentialID        string `json:"credential_id"`
		Label               string `json:"label"`
		Fingerprint         string `json:"fingerprint"`
		CertificateNotAfter string `json:"certificate_not_after"`
	}
	out := struct {
		Principal  principalIdentityJSON  `json:"principal"`
		Credential credentialIdentityJSON `json:"credential"`
		Roles      []roleJSON             `json:"roles"`
	}{Roles: make([]roleJSON, 0, len(response.GetRoles()))}
	if principal := response.GetPrincipal(); principal != nil {
		out.Principal = principalIdentityJSON{PrincipalID: principal.GetPrincipalID(), Name: principal.GetName(), DisplayName: principal.GetDisplayName(), Kind: principal.GetKind()}
	}
	if credential := response.GetCredential(); credential != nil {
		out.Credential = credentialIdentityJSON{CredentialID: credential.GetCredentialID(), Label: credential.GetLabel(), Fingerprint: credential.GetFingerprint(), CertificateNotAfter: FormatProtoTimestamp(credential.GetCertificateNotAfter())}
	}
	for _, role := range response.GetRoles() {
		out.Roles = append(out.Roles, roleJSON{Role: role.GetRole(), ScopeType: role.GetScopeType(), Namespace: role.GetNamespace()})
	}
	return PrintJSON(w, out)
}

func principalJSON(principal *adminv1.Principal) *PrincipalJSON {
	if principal == nil {
		return nil
	}
	return &PrincipalJSON{PrincipalID: principal.GetPrincipalID(), Name: principal.GetName(), DisplayName: principal.GetDisplayName(), Kind: accessEnumLabel(principal.GetKind().String()), Status: accessEnumLabel(principal.GetStatus().String()), Version: principal.GetVersion(), CreatedAt: FormatProtoTimestamp(principal.GetCreatedAt()), UpdatedAt: FormatProtoTimestamp(principal.GetUpdatedAt())}
}

func credentialJSON(credential *adminv1.PrincipalCredential) *PrincipalCredentialJSON {
	if credential == nil {
		return nil
	}
	return &PrincipalCredentialJSON{CredentialID: credential.GetCredentialID(), PrincipalID: credential.GetPrincipalID(), Fingerprint: credential.GetFingerprint(), CertificateNotAfter: FormatProtoTimestamp(credential.GetCertificateNotAfter()), Label: credential.GetLabel(), CreatedAt: FormatProtoTimestamp(credential.GetCreatedAt()), RevokedAt: FormatProtoTimestamp(credential.GetRevokedAt())}
}

func bindingJSON(binding *adminv1.RoleBinding) *RoleBindingJSON {
	if binding == nil {
		return nil
	}
	return &RoleBindingJSON{BindingID: binding.GetBindingID(), PrincipalID: binding.GetPrincipalID(), ScopeType: accessEnumLabel(binding.GetScopeType().String()), Namespace: binding.GetNamespace(), Role: accessEnumLabel(binding.GetRole().String()), CreatedByPrincipalID: binding.GetCreatedByPrincipalID(), CreatedAt: FormatProtoTimestamp(binding.GetCreatedAt()), RevokedByPrincipalID: binding.GetRevokedByPrincipalID(), RevokedAt: FormatProtoTimestamp(binding.GetRevokedAt())}
}

func accessEnumLabel(value string) string {
	for _, prefix := range []string{"PRINCIPAL_KIND_", "PRINCIPAL_STATUS_", "ACCESS_SCOPE_TYPE_", "ACCESS_ROLE_"} {
		value = strings.TrimPrefix(value, prefix)
	}
	return strings.ToLower(value)
}
