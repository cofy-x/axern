package admin

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Access struct{ client adminv1.AccessAdminClient }

func NewAccess(client adminv1.AccessAdminClient) *Access { return &Access{client: client} }

func (a *Access) AddCredential(ctx context.Context, principalID, certificatePath, sshKeyPath, expiry, label string) (*adminv1.AddPrincipalCredentialResponse, error) {
	if (certificatePath == "") == (sshKeyPath == "") {
		return nil, errors.New("exactly one of --certificate or --ssh-public-key is required")
	}
	req := &adminv1.AddPrincipalCredentialRequest{PrincipalID: strings.TrimSpace(principalID), Label: strings.TrimSpace(label)}
	if certificatePath != "" {
		if expiry != "" {
			return nil, errors.New("certificate expiry cannot be overridden")
		}
		der, err := readCertificateDER(certificatePath)
		if err != nil {
			return nil, err
		}
		req.CertificateDer = der
	} else {
		deadline, err := time.Parse(time.RFC3339, expiry)
		if err != nil {
			return nil, errors.New("SSH credentials require --expires-at in RFC3339 format")
		}
		key, err := os.ReadFile(sshKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read SSH public key: %w", err)
		}
		req.SshPublicKey = string(key)
		req.ExpiresAt = timestamppb.New(deadline)
	}
	return a.client.AddPrincipalCredential(ctx, req)
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
