package doctorcmd

import (
	"fmt"
	"strings"

	appdoctor "github.com/cofy-x/axern/apps/cli/internal/application/doctor"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/spf13/cobra"
)

func renderTable(cmd *cobra.Command, report appdoctor.Report) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Axern doctor: %s\n", report.Status)
	if report.Context != "" {
		fmt.Fprintf(w, "Context: %s\n", report.Context)
	}
	fmt.Fprintf(w, "Namespace: %s\n", report.Namespace)
	fmt.Fprintf(w, "Mode: %s\n\n", strings.ReplaceAll(report.Mode, "_", "-"))
	rows := make([][]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		rows = append(rows, []string{
			check.Name,
			string(check.Status),
			check.Code,
			fmt.Sprintf("%dms", check.DurationMS),
			check.Message,
			check.Remediation,
		})
	}
	output.RenderTable(w, []string{"CHECK", "STATUS", "CODE", "LATENCY", "MESSAGE", "REMEDIATION"}, rows)
}
