package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPrintRoleBindingJSONUsesStablePublicNames(t *testing.T) {
	createdAt := time.Date(2026, time.July, 30, 8, 9, 10, 0, time.UTC)
	var output bytes.Buffer
	err := PrintRoleBindingJSON(&output, &adminv1.RoleBinding{
		BindingID:   "rb-1",
		PrincipalID: "prn-1",
		ScopeType:   adminv1.AccessScopeType_ACCESS_SCOPE_TYPE_NAMESPACE,
		Namespace:   "default",
		Role:        adminv1.AccessRole_ACCESS_ROLE_NAMESPACE_EDITOR,
		CreatedAt:   timestamppb.New(createdAt),
	})
	if err != nil {
		t.Fatalf("PrintRoleBindingJSON() error = %v", err)
	}
	for _, want := range []string{
		`"scope_type": "namespace"`,
		`"role": "namespace_editor"`,
		`"created_at": "2026-07-30T08:09:10Z"`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output %q does not contain %q", output.String(), want)
		}
	}
}
