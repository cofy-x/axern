package doctorcmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	appdoctor "github.com/cofy-x/axern/apps/cli/internal/application/doctor"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	defaultTemplateID   = "python311"
	defaultRuntimeClass = "runsc"
	degradedExitCode    = 1
	invalidExitCode     = 2
	failedExitCode      = 3
)

type options struct {
	namespace    string
	probe        bool
	templateID   string
	runtimeClass string
	checkTimeout time.Duration
	probeTimeout time.Duration
}

func Command(runtime command.Runtime) *cobra.Command {
	values := options{
		namespace:    "default",
		templateID:   defaultTemplateID,
		runtimeClass: defaultRuntimeClass,
		checkTimeout: 15 * time.Second,
		probeTimeout: 5 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose Axern configuration, control-plane access, and optional data-plane execution",
		Args:  command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := values.validate(cmd); err != nil {
				return command.Usage(err)
			}
			connection, err := runtime.ResolveConnection()
			if err != nil {
				return renderAndExit(cmd, runtime, appdoctor.ConfigurationFailure("", values.namespace, values.probe))
			}
			var probe *appdoctor.ProbeOptions
			if values.probe {
				probe = &appdoctor.ProbeOptions{
					TemplateID:   strings.TrimSpace(values.templateID),
					RuntimeClass: strings.TrimSpace(values.runtimeClass),
					Timeout:      values.probeTimeout,
				}
			}
			control := appdoctor.New(appdoctor.Options{
				ContextName:  connection.ContextName,
				Namespace:    strings.TrimSpace(values.namespace),
				TLS:          appdoctor.TLSConfig{CACert: connection.Config.TLSCACert, Cert: connection.Config.TLSCert, Key: connection.Config.TLSKey},
				Probe:        probe,
				CheckTimeout: values.checkTimeout,
				Open: func(ctx context.Context) (*appdoctor.Session, error) {
					session, openErr := connection.Open(ctx)
					if openErr != nil {
						return nil, openErr
					}
					return &appdoctor.Session{
						Context: session.Context, Namespace: session.Clients.Namespace,
						Catalog: session.Clients.Catalog, Environment: session.Clients.Environment,
						Run: session.Clients.Run, Close: session.Close,
					}, nil
				},
			})
			return renderAndExit(cmd, runtime, control.Diagnose(cmd.Context()))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&values.namespace, "namespace", values.namespace, "namespace to validate and use for the optional probe")
	flags.BoolVar(&values.probe, "probe", false, "create a temporary Environment and execute a catalog-backed Run")
	flags.StringVar(&values.templateID, "template-id", values.templateID, "runtime template used by --probe")
	flags.StringVar(&values.runtimeClass, "runtime-class", values.runtimeClass, "runtime class used by --probe")
	flags.DurationVar(&values.checkTimeout, "check-timeout", values.checkTimeout, "timeout for each read-only API check")
	flags.DurationVar(&values.probeTimeout, "probe-timeout", values.probeTimeout, "timeout for data-plane execution")
	return cmd
}

func (o options) validate(cmd *cobra.Command) error {
	if strings.TrimSpace(o.namespace) == "" {
		return fmt.Errorf("--namespace is required")
	}
	if o.checkTimeout <= 0 {
		return fmt.Errorf("--check-timeout must be positive")
	}
	if o.probeTimeout <= 0 {
		return fmt.Errorf("--probe-timeout must be positive")
	}
	for _, name := range []string{"template-id", "runtime-class", "probe-timeout"} {
		if cmd.Flags().Changed(name) && !o.probe {
			return fmt.Errorf("--%s requires --probe", name)
		}
	}
	if o.probe && strings.TrimSpace(o.templateID) == "" {
		return fmt.Errorf("--template-id is required with --probe")
	}
	if o.probe && strings.TrimSpace(o.runtimeClass) == "" {
		return fmt.Errorf("--runtime-class is required with --probe")
	}
	return nil
}

func renderAndExit(cmd *cobra.Command, runtime command.Runtime, report appdoctor.Report) error {
	format, err := runtime.Format()
	if err != nil {
		return err
	}
	if format == output.FormatJSON {
		if err := output.PrintJSON(cmd.OutOrStdout(), report); err != nil {
			return err
		}
	} else {
		renderTable(cmd, report)
	}
	switch report.Status {
	case appdoctor.StatusHealthy:
		return nil
	case appdoctor.StatusDegraded:
		return command.ExitError{Code: degradedExitCode}
	default:
		if configurationFailed(report) {
			return command.ExitError{Code: invalidExitCode}
		}
		return command.ExitError{Code: failedExitCode}
	}
}

func configurationFailed(report appdoctor.Report) bool {
	for _, check := range report.Checks {
		if check.Name == "configuration" {
			return check.Status == appdoctor.CheckFail
		}
	}
	return false
}
