package access

import "testing"

func TestAuthorizeRoleMatrix(t *testing.T) {
	bindings := map[Role]Binding{
		RolePlatformAdmin:   {Role: RolePlatformAdmin, Scope: ScopePlatform},
		RoleNamespaceAdmin:  {Role: RoleNamespaceAdmin, Scope: ScopeNamespace, Namespace: "team-a"},
		RoleNamespaceEditor: {Role: RoleNamespaceEditor, Scope: ScopeNamespace, Namespace: "team-a"},
		RoleNamespaceViewer: {Role: RoleNamespaceViewer, Scope: ScopeNamespace, Namespace: "team-a"},
	}
	cases := []struct {
		role   Role
		action Action
		ns     string
		want   bool
	}{
		{RolePlatformAdmin, ActionPlatformAdmin, "", true},
		{RoleNamespaceAdmin, ActionResourceWrite, "team-a", true},
		{RoleNamespaceAdmin, ActionPlatformAdmin, "team-a", false},
		{RoleNamespaceEditor, ActionSandboxExecute, "team-a", true},
		{RoleNamespaceEditor, ActionNamespaceAccess, "team-a", false},
		{RoleNamespaceViewer, ActionResourceRead, "team-a", true},
		{RoleNamespaceViewer, ActionResourceWrite, "team-a", false},
		{RoleNamespaceAdmin, ActionResourceRead, "team-b", false},
	}
	for _, tc := range cases {
		actor := Actor{Principal: Principal{Status: PrincipalStatusActive}, Bindings: []Binding{bindings[tc.role]}}
		if got := Authorize(actor, tc.action, tc.ns); got != tc.want {
			t.Errorf("Authorize(%s, %s, %s) = %v, want %v", tc.role, tc.action, tc.ns, got, tc.want)
		}
	}
}

func TestCanGrantPreventsEscalation(t *testing.T) {
	actor := Actor{Principal: Principal{Status: PrincipalStatusActive}, Bindings: []Binding{{Role: RoleNamespaceAdmin, Namespace: "team-a"}}}
	if !CanGrant(actor, RoleNamespaceAdmin, "team-a") {
		t.Fatal("namespace admin could not grant namespace role")
	}
	if CanGrant(actor, RolePlatformAdmin, "team-a") || CanGrant(actor, RoleNamespaceViewer, "team-b") || CanGrant(actor, RoleRolloutExecutor, "team-a") {
		t.Fatal("namespace admin could escalate privileges")
	}
}

func TestRolloutDelegationIsLimitedToDataPlaneActions(t *testing.T) {
	for _, action := range []Action{ActionResourceRead, ActionResourceWrite, ActionSandboxExecute} {
		if !IsRolloutDelegatableAction(action) {
			t.Errorf("action %s should be delegatable", action)
		}
	}
	for _, action := range []Action{ActionNamespaceManage, ActionQuotaManage, ActionNamespaceAccess, ActionPlatformAdmin} {
		if IsRolloutDelegatableAction(action) {
			t.Errorf("action %s must not be delegatable", action)
		}
	}
}
