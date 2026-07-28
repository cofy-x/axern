package export

import (
	"github.com/cofy-x/axern/apps/axrun/internal/application/exportdata"
	"github.com/cofy-x/axern/apps/axrun/internal/command"
	"github.com/spf13/cobra"
)

func Command(options *command.Options) *cobra.Command {
	root := &cobra.Command{Use: "export", Short: "Export rollout data"}
	for _, item := range []struct {
		name   string
		format exportdata.Format
	}{{"sft", exportdata.FormatSFT}, {"reward", exportdata.FormatReward}, {"trace", exportdata.FormatTrace}, {"preference", exportdata.FormatPreference}} {
		item := item
		var outputFile string
		cmd := &cobra.Command{
			Use: item.name + " <run-dir>", Args: command.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				result, err := exportdata.Export(exportdata.Params{RunDir: args[0], OutputPath: outputFile, Format: item.format})
				if err != nil {
					return err
				}
				return command.PrintValue(cmd.OutOrStdout(), options.Output, result, "records=%d file=%s\n", result.RecordCount, result.OutputPath)
			},
		}
		cmd.Flags().StringVar(&outputFile, "output-file", "", "export output file")
		root.AddCommand(cmd)
	}
	return root
}
