package admin

import (
	"context"
	"fmt"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc"
)

const (
	AuditOperationForceAllocationLifecycleRetry = "force-allocation-lifecycle-retry"
	AuditOperationFailAllocationLifecycleRetry  = "fail-allocation-lifecycle-retry"
	AuditOperationClearAllocationLifecycleRetry = "clear-allocation-lifecycle-retry"
	AuditOperationRetryStorageBinding           = "retry-storage-binding"
	AuditOperationPurgeService                  = "purge-service"
	AuditOperationRetireNode                    = "retire-node"
	AuditOperationCreatePrincipal               = "create-principal"
	AuditOperationDisablePrincipal              = "disable-principal"
	AuditOperationAddCredential                 = "add-credential"
	AuditOperationRevokeCredential              = "revoke-credential"
	AuditOperationGrantRoleBinding              = "grant-role-binding"
	AuditOperationRevokeRoleBinding             = "revoke-role-binding"
	AuditOperationBootstrapAccess               = "bootstrap-access"

	AuditTargetTypeAllocation     = "allocation"
	AuditTargetTypeStorageBinding = "storage-binding"
	AuditTargetTypeService        = "service"
	AuditTargetTypeNode           = "node"
	AuditTargetTypePrincipal      = "principal"
	AuditTargetTypeCredential     = "credential"
	AuditTargetTypeRoleBinding    = "role-binding"
)

type AuditClient interface {
	ListAdminAuditEvents(context.Context, *adminv1.ListAdminAuditEventsRequest, ...grpc.CallOption) (*adminv1.ListAdminAuditEventsResponse, error)
}

type AuditControl struct {
	client AuditClient
}

type AuditListOptions struct {
	Operation  string
	TargetType string
	TargetID   string
	Limit      int
}

func NewAudit(client AuditClient) AuditControl {
	return AuditControl{client: client}
}

func (c AuditControl) ListEvents(ctx context.Context, options AuditListOptions) (*adminv1.ListAdminAuditEventsResponse, error) {
	return c.client.ListAdminAuditEvents(ctx, &adminv1.ListAdminAuditEventsRequest{
		Filter: &adminv1.AdminAuditEventFilter{
			Operation:  ParseAuditOperation(options.Operation),
			TargetType: ParseAuditTargetType(options.TargetType),
			TargetID:   strings.TrimSpace(options.TargetID),
		},
		Limit: int32(options.Limit),
	})
}

func ParseAuditOperation(value string) adminv1.AdminAuditOperation {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AuditOperationForceAllocationLifecycleRetry:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FORCE_ALLOCATION_LIFECYCLE_RETRY
	case AuditOperationFailAllocationLifecycleRetry:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FAIL_ALLOCATION_LIFECYCLE_RETRY
	case AuditOperationClearAllocationLifecycleRetry:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_CLEAR_ALLOCATION_LIFECYCLE_RETRY
	case AuditOperationRetryStorageBinding:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_RETRY_STORAGE_BINDING
	case AuditOperationPurgeService:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_PURGE_SERVICE
	case AuditOperationRetireNode:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_RETIRE_NODE
	case AuditOperationCreatePrincipal:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_CREATE_PRINCIPAL
	case AuditOperationDisablePrincipal:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_DISABLE_PRINCIPAL
	case AuditOperationAddCredential:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_ADD_CREDENTIAL
	case AuditOperationRevokeCredential:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_REVOKE_CREDENTIAL
	case AuditOperationGrantRoleBinding:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_GRANT_ROLE_BINDING
	case AuditOperationRevokeRoleBinding:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_REVOKE_ROLE_BINDING
	case AuditOperationBootstrapAccess:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_BOOTSTRAP_ACCESS
	default:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_UNSPECIFIED
	}
}

func ValidateAuditOperation(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if ParseAuditOperation(value) == adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_UNSPECIFIED {
		return fmt.Errorf("invalid --operation %q", value)
	}
	return nil
}

func ParseAuditTargetType(value string) adminv1.AdminAuditTargetType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AuditTargetTypeAllocation:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_ALLOCATION
	case AuditTargetTypeStorageBinding:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_STORAGE_BINDING
	case AuditTargetTypeService:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_SERVICE
	case AuditTargetTypeNode:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_NODE
	case AuditTargetTypePrincipal:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_PRINCIPAL
	case AuditTargetTypeCredential:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_CREDENTIAL
	case AuditTargetTypeRoleBinding:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_ROLE_BINDING
	default:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_UNSPECIFIED
	}
}

func ValidateAuditTargetType(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if ParseAuditTargetType(value) == adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_UNSPECIFIED {
		return fmt.Errorf("invalid --target-type %q", value)
	}
	return nil
}

func ValidateAuditTargetFilter(targetType string, targetID string) error {
	if strings.TrimSpace(targetID) != "" && strings.TrimSpace(targetType) == "" {
		return fmt.Errorf("--target-type is required when --target-id is set")
	}
	return nil
}
