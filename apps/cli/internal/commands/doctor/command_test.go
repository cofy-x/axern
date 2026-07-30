package doctorcmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	appdoctor "github.com/cofy-x/axern/apps/cli/internal/application/doctor"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/spf13/cobra"
)

func TestProbeSpecificFlagsRequireProbe(t *testing.T) {
	options := &command.Options{Output: "table"}
	runtime := command.Runtime{Options: options, Root: &cobra.Command{}}
	cmd := Command(runtime)
	cmd.SetArgs([]string{"--template-id", "python311"})

	err := cmd.Execute()
	var usage command.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "requires --probe") {
		t.Fatalf("error = %v, want probe usage error", err)
	}
}

func TestRenderJSONReturnsDistinctFailedExitCode(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	runtime := command.Runtime{Options: &command.Options{Output: "json"}}
	report := appdoctor.Report{
		Status: appdoctor.StatusFailed, Namespace: "default", Mode: "read_only",
		Checks: []appdoctor.Check{{Name: "gateway", Status: appdoctor.CheckFail, Code: "gateway_unreachable", Message: "gateway connection failed"}},
	}

	err := renderAndExit(cmd, runtime, report)
	var exit command.ExitError
	if !errors.As(err, &exit) || exit.Code != failedExitCode {
		t.Fatalf("error = %#v, want exit code %d", err, failedExitCode)
	}
	if !strings.Contains(out.String(), `"status": "failed"`) || !strings.Contains(out.String(), `"gateway_unreachable"`) {
		t.Fatalf("JSON output = %s", out.String())
	}
}

func TestRenderConfigurationFailureReturnsInvalidExitCode(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	runtime := command.Runtime{Options: &command.Options{Output: "json"}}
	report := appdoctor.ConfigurationFailure("", "default", false)

	err := renderAndExit(cmd, runtime, report)
	var exit command.ExitError
	if !errors.As(err, &exit) || exit.Code != invalidExitCode {
		t.Fatalf("error = %#v, want exit code %d", err, invalidExitCode)
	}
	if !strings.Contains(out.String(), `"connection_configuration_invalid"`) {
		t.Fatalf("JSON output = %s", out.String())
	}
}
