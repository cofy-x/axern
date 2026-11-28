package nativebackup

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRequiresExplicitWorkspaceInventory(t *testing.T) {
	const header = `{"format":"axern.native-node-restore.v1","old_node":"source-node","new_node":"replacement-node"`
	for _, test := range []struct {
		name  string
		input string
		valid bool
	}{
		{name: "empty", input: header + `,"workspaces":[]}`, valid: true},
		{name: "missing", input: header + `}`},
		{name: "null", input: header + `,"workspaces":null}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, mapped := range []bool{false, true} {
				plan, err := Decode(strings.NewReader(test.input), mapped)
				if !test.valid {
					if err == nil || !strings.Contains(err.Error(), "explicit retained Workspace inventory") {
						t.Fatalf("mapped %t accepted missing inventory or returned an unexpected error: %v", mapped, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("mapped %t rejected the explicit empty inventory: %v", mapped, err)
				}
				if plan.Workspaces == nil || len(plan.Workspaces) != 0 {
					t.Fatalf("mapped %t changed the empty inventory: %#v", mapped, plan.Workspaces)
				}
				raw, err := json.Marshal(plan)
				if err != nil || string(raw) != test.input {
					t.Fatalf("mapped %t did not round-trip the inventory: %s, %v", mapped, raw, err)
				}
			}
		})
	}
}
