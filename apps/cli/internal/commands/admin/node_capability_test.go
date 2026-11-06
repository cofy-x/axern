package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appadmin "github.com/cofy-x/axern/apps/cli/internal/application/admin"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/controlv1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNodeCapabilityCheckExitCodesAndJSON(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result appadmin.NodeCapabilityCheckResult
		err    error
		code   int
	}{
		{name: "ready", result: appadmin.NodeCapabilityCheckResult{Ready: true}},
		{name: "expired evidence", result: appadmin.NodeCapabilityCheckResult{Reason: "evidence expired"}, code: 1},
		{name: "missing snapshot", err: status.Error(codes.NotFound, "not reported"), code: 1},
		{name: "temporary network failure", err: status.Error(codes.Unavailable, "not connected"), code: 1},
		{name: "unauthenticated", err: status.Error(codes.Unauthenticated, "private-upstream-error-detail"), code: 2},
		{name: "permission denied", err: status.Error(codes.PermissionDenied, "permission denied"), code: 2},
		{name: "invalid connection material", err: controlv1.ConfigurationError{Err: errors.New("private-upstream-error-detail")}, code: 2},
		{name: "invalid context", err: command.Usage(errors.New("invalid context")), code: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			err := renderNodeCapabilityCheck(cmd, command.Runtime{Options: &command.Options{Output: "json"}}, tc.result, tc.err)
			if tc.code == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var exit command.ExitError
				if !errors.As(err, &exit) || exit.Code != tc.code {
					t.Fatalf("error = %v, want exit %d", err, tc.code)
				}
			}
			var rendered struct {
				Ready  bool   `json:"ready"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(output.Bytes(), &rendered); err != nil {
				t.Fatal(err)
			}
			if rendered.Ready != (tc.code == 0) || (tc.code != 0 && rendered.Reason == "") {
				t.Fatalf("unexpected report: %s", output.String())
			}
			if strings.Contains(output.String(), "private-upstream-error-detail") {
				t.Fatalf("report disclosed raw error details: %s", output.String())
			}
		})
	}
}

func TestNodeCapabilityCheckValidatesFlagsBeforeOpeningConnection(t *testing.T) {
	for _, args := range [][]string{
		{"node-test"},
		{"node-test", "--platform", "NETWORK_BRIDGE"},
		{"node-test", "--platform", "UNKNOWN", "--boot-id", "11111111-2222-3333-4444-555555555555"},
	} {
		cmd := nodeCapabilityCheckCommand(command.Runtime{})
		cmd.SetArgs(args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		var usage command.UsageError
		if err := cmd.Execute(); !errors.As(err, &usage) {
			t.Fatalf("args %v: error = %v, want usage error", args, err)
		}
	}
}
