package local

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	applocal "github.com/cofy-x/axern/apps/cli/internal/localruntime"
	"github.com/spf13/cobra"
)

func TestLocalLifecycleSurface(t *testing.T) {
	cmd := Command(command.Runtime{}, "1.2.3")
	for _, name := range []string{"up", "status", "logs", "doctor", "down", "reset", "upgrade", "path", "image"} {
		found, _, err := cmd.Find([]string{name})
		if err != nil || found == cmd {
			t.Fatalf("local subcommand %q is missing: %v", name, err)
		}
	}
	doctor, _, err := cmd.Find([]string{"doctor"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"probe", "dns-query-name", "check-timeout", "probe-timeout", "template-id", "runtime-class"} {
		if doctor.Flags().Lookup(name) == nil {
			t.Fatalf("local doctor flag --%s is missing", name)
		}
	}
}

func TestRenderLocalDoctorTableIncludesStableContractFields(t *testing.T) {
	var buffer bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buffer)
	renderLocalDoctorTable(cmd, applocal.DoctorReport{
		Status: "degraded",
		Mode:   "read_only",
		Checks: []applocal.Check{{
			Name: "runtime_dns_node", Status: "warn", Code: "runtime_dns_node_partial", DurationMS: 17,
			Message: "partial resolver reachability", Remediation: "repair the resolver set",
		}},
	})
	output := buffer.String()
	for _, value := range []string{
		"Axern local doctor: degraded", "Mode: read-only", "CHECK", "STATUS", "CODE", "LATENCY",
		"runtime_dns_node", "warn", "runtime_dns_node_partial", "17ms", "repair the resolver set",
	} {
		if !strings.Contains(output, value) {
			t.Fatalf("table output does not contain %q:\n%s", value, output)
		}
	}
}
