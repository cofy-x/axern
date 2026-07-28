package task

import (
	"os"

	"github.com/cofy-x/axern/apps/axrun/internal/command"
	"github.com/cofy-x/axern/apps/axrun/internal/taskset"
	"github.com/spf13/cobra"
)

func Command(options *command.Options) *cobra.Command {
	root := &cobra.Command{Use: "task", Short: "Build and publish immutable TaskSets"}
	var init taskset.InitParams
	initCommand := &cobra.Command{
		Use: "init", Short: "Create an axrun/v1 TaskSetBuild project", Args: command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := taskset.Init(init)
			if err != nil {
				return command.Usage(err)
			}
			return command.PrintValue(cmd.OutOrStdout(), options.Output, result, "project=%s build_file=%s\n", result.OutputDir, result.BuildFile)
		},
	}
	initCommand.Flags().StringVar(&init.OutputDir, "output-dir", "axrun-taskset", "project output directory")

	var buildFile, buildOutput string
	build := &cobra.Command{
		Use: "build", Short: "Compile an axrun/v1 TaskSetBuild into a deterministic local bundle", Args: command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := taskset.Build(taskset.BuildParams{File: buildFile, Output: buildOutput})
			if err != nil {
				return command.Usage(err)
			}
			return command.PrintValue(cmd.OutOrStdout(), options.Output, result, "tasks=%d digest=%s bundle=%s\n", result.TaskCount, result.SourceDigest, result.Output)
		},
	}
	build.Flags().StringVarP(&buildFile, "file", "f", "", "path to an axrun/v1 TaskSetBuild spec")
	build.Flags().StringVar(&buildOutput, "output", "", "local TaskSet bundle directory")
	_ = build.MarkFlagRequired("file")
	_ = build.MarkFlagRequired("output")

	inspect := &cobra.Command{
		Use: "inspect <local-path-or-oci-ref>", Short: "Inspect a local or published TaskSet descriptor", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := taskset.ResolveContext(cmd.Context(), args[0])
			if err != nil {
				return command.Usage(err)
			}
			return command.PrintValue(cmd.OutOrStdout(), options.Output, result.Descriptor, "name=%s tasks=%d source_digest=%s descriptor_digest=%s\n", result.Descriptor.Name, len(result.Tasks), result.Descriptor.SourceDigest, result.DescriptorDigest)
		},
	}

	var publish taskset.PublishParams
	publishCommand := &cobra.Command{
		Use: "publish <bundle-dir>", Short: "Publish a TaskSet payload and immutable descriptor", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			publish.Bundle = args[0]
			if publish.KovaToken == "" {
				publish.KovaToken = os.Getenv("KOVA_TOKEN")
			}
			result, err := taskset.Publish(cmd.Context(), publish)
			if err != nil {
				return err
			}
			return command.PrintValue(cmd.OutOrStdout(), options.Output, result, "descriptor=%s source_digest=%s\n", result.DescriptorReference, result.SourceDigest)
		},
	}
	publishCommand.Flags().StringVar(&publish.Target, "target", "", "registry repository")
	publishCommand.Flags().StringVar(&publish.Publisher, "publisher", "kova", "publisher backend: kova or local")
	publishCommand.Flags().StringVar(&publish.KovaEndpoint, "kova-endpoint", os.Getenv("KOVA_ENDPOINT"), "Kova service endpoint")
	publishCommand.Flags().BoolVar(&publish.Preheat, "preheat", false, "preheat published payloads")
	_ = publishCommand.MarkFlagRequired("target")

	root.AddCommand(initCommand, build, publishCommand, inspect)
	return root
}
