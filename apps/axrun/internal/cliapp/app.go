package cliapp

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/apps/axrun/internal/command"
	exportcmd "github.com/cofy-x/axern/apps/axrun/internal/commands/export"
	managedcmd "github.com/cofy-x/axern/apps/axrun/internal/commands/managed"
	profilecmd "github.com/cofy-x/axern/apps/axrun/internal/commands/profile"
	servecmd "github.com/cofy-x/axern/apps/axrun/internal/commands/serve"
	taskcmd "github.com/cofy-x/axern/apps/axrun/internal/commands/task"
	validatecmd "github.com/cofy-x/axern/apps/axrun/internal/commands/validate"
	workercmd "github.com/cofy-x/axern/apps/axrun/internal/commands/worker"
	"github.com/cofy-x/axern/lib/go/clientconfig"
	"github.com/spf13/cobra"
)

type UsageError = command.UsageError
type ExitError = command.ExitError

func New(version string) *cobra.Command {
	options := &command.Options{ConfigPath: clientconfig.DefaultPath(), Output: "table"}
	root := &cobra.Command{
		Use: "axrun", Short: "Run reproducible agent rollouts on Axern", Version: version,
		SilenceErrors: true, SilenceUsage: true,
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return command.Usage(err) })
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if options.Output != "table" && options.Output != "json" {
			return command.Usage(fmt.Errorf("output must be table or json"))
		}
		return nil
	}
	root.PersistentFlags().StringVar(&options.ConfigPath, "config", options.ConfigPath, "path to the Axern context file")
	root.PersistentFlags().StringVar(&options.ContextName, "context", "", "Axern context name")
	root.PersistentFlags().StringVar(&options.Output, "format", "table", "output format: table or json")
	root.AddCommand(
		taskcmd.Command(options),
		managedcmd.Command(options),
		profilecmd.Command(options),
		workercmd.Command(options),
		validatecmd.Command(options),
		exportcmd.Command(options),
		servecmd.Command(),
	)
	root.InitDefaultCompletionCmd()
	return root
}

func Execute(root *cobra.Command, args []string) error {
	root.SetArgs(args)
	if _, _, err := root.Find(args); err != nil {
		return command.Usage(err)
	}
	return root.Execute()
}
