package admin

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
)

type Access struct{ client adminv1.AccessAdminClient }

func NewAccess(client adminv1.AccessAdminClient) *Access { return &Access{client: client} }

func (a *Access) AddCredential(ctx context.Context, principalID, certificatePath, label string) (*adminv1.AddPrincipalCredentialResponse, error) {
	der, err := readCertificateDER(certificatePath)
	if err != nil {
		return nil, err
	}
	return a.client.AddPrincipalCredential(ctx, &adminv1.AddPrincipalCredentialRequest{PrincipalID: strings.TrimSpace(principalID), CertificateDer: der, Label: strings.TrimSpace(label)})
}

func readCertificateDER(path string) ([]byte, error) {
	contents, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("read certificate: %w", err)
	}
	block, _ := pem.Decode(contents)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("certificate must be PEM encoded")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return certificate.Raw, nil
}

func PrincipalKind(value string) (adminv1.PrincipalKind, error) {
	switch strings.TrimSpace(value) {
	case "human":
		return adminv1.PrincipalKind_PRINCIPAL_KIND_HUMAN, nil
	case "service":
		return adminv1.PrincipalKind_PRINCIPAL_KIND_SERVICE, nil
	default:
		return 0, errors.New("principal kind must be human or service")
	}
}
func ScopeAndRole(scope, namespace, role string) (adminv1.AccessScopeType, adminv1.AccessRole, error) {
	var s adminv1.AccessScopeType
	switch strings.TrimSpace(scope) {
	case "platform":
		s = adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_PLATFORM
	case "namespace":
		s = adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_NAMESPACE
	default:
		return 0, 0, errors.New("scope must be platform or namespace")
	}
	roles := map[string]adminv1.AccessRole{"platform_admin": adminv1.AccessRole_ACCESS_ROLE_PLATFORM_ADMIN, "namespace_admin": adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_ADMIN, "namespace_editor": adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_EDITOR, "namespace_viewer": adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_VIEWER}
	r, ok := roles[strings.TrimSpace(role)]
	if !ok {
		return 0, 0, errors.New("invalid access role")
	}
	if (s == adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_PLATFORM) != (r == adminv1.AccessRole_ACCESS_ROLE_PLATFORM_ADMIN) {
		return 0, 0, errors.New("platform scope requires platform_admin; namespace roles require namespace scope")
	}
	if s == adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_PLATFORM && strings.TrimSpace(namespace) != "" {
		return 0, 0, errors.New("platform scope does not accept a namespace")
	}
	if s == adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_NAMESPACE && strings.TrimSpace(namespace) == "" {
		return 0, 0, errors.New("namespace scope requires --namespace")
	}
	return s, r, nil
}
