package access

func Authorize(actor Actor, action Action, namespace string) bool {
	if actor.Principal.Status != PrincipalStatusActive {
		return false
	}
	if action == ActionIdentityRead || action == ActionCatalogRead {
		return true
	}
	for _, binding := range actor.Bindings {
		if binding.RevokedAt != nil {
			continue
		}
		if allows(binding, action, namespace) {
			return true
		}
	}
	return false
}

func allows(binding Binding, action Action, namespace string) bool {
	switch binding.Role {
	case RolePlatformAdmin:
		return true
	case RoleRolloutExecutor:
		return action == ActionRolloutWorkExecute
	case RoleNamespaceAdmin:
		if binding.Namespace != namespace {
			return false
		}
		return action == ActionNamespaceRead || action == ActionQuotaRead || action == ActionResourceRead ||
			action == ActionResourceWrite || action == ActionSandboxExecute || action == ActionNamespaceAccess
	case RoleNamespaceEditor:
		if binding.Namespace != namespace {
			return false
		}
		return action == ActionNamespaceRead || action == ActionQuotaRead || action == ActionResourceRead ||
			action == ActionResourceWrite || action == ActionSandboxExecute
	case RoleNamespaceViewer:
		if binding.Namespace != namespace {
			return false
		}
		return action == ActionNamespaceRead || action == ActionQuotaRead || action == ActionResourceRead
	default:
		return false
	}
}

func CanGrant(actor Actor, role Role, namespace string) bool {
	if !IsPublicRole(role) {
		return false
	}
	if Authorize(actor, ActionPlatformAccess, "") {
		return true
	}
	if role == RolePlatformAdmin {
		return false
	}
	return Authorize(actor, ActionNamespaceAccess, namespace)
}

func CanReadNamespace(actor Actor, namespace string) bool {
	return Authorize(actor, ActionNamespaceRead, namespace)
}

func HasRole(actor Actor, role Role) bool {
	for _, binding := range actor.Bindings {
		if binding.RevokedAt == nil && binding.Role == role {
			return true
		}
	}
	return false
}

func IsRolloutDelegatableAction(action Action) bool {
	switch action {
	case ActionResourceRead, ActionResourceWrite, ActionSandboxExecute:
		return true
	default:
		return false
	}
}
