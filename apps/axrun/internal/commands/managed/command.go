package managed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	managedrollout "github.com/cofy-x/axern/apps/axrun/internal/application/managedrollout"
	"github.com/cofy-x/axern/apps/axrun/internal/command"
	"github.com/cofy-x/axern/apps/axrun/internal/rolloutspec"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type untilMode string

const (
	untilReady    untilMode = "ready"
	untilTerminal untilMode = "terminal"
)

func Command(options *command.Options) *cobra.Command {
	root := &cobra.Command{Use: "rollout", Short: "Plan, run, and inspect durable Axern rollouts"}
	root.AddCommand(plan(options), start(options), run(options), get(options), list(options), watchCommand(options), inspect(options), cancel(options), retry(options), deleteRollout(options), artifact(options), compare(options))
	return root
}

func client(cmd *cobra.Command, options *command.Options) (rolloutv1.RolloutControlClient, io.Closer, error) {
	sdk, err := options.ControlClient(cmd.Context())
	if err != nil {
		return nil, nil, err
	}
	return sdk.RolloutControl(), sdk, nil
}

type submitConfig struct {
	Policy      rolloutv1.RolloutStartPolicy
	Use         string
	Short       string
	AllowDetach bool
}

func submit(options *command.Options, config submitConfig) *cobra.Command {
	var file, key string
	var detach bool
	cmd := &cobra.Command{Use: config.Use, Short: config.Short, Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(file) == "" {
			return command.Usage(fmt.Errorf("--file is required"))
		}
		envelope, err := rolloutspec.Load(file)
		if err != nil {
			return command.Usage(err)
		}
		request, err := envelope.ControlRequest(key)
		if err != nil {
			return command.Usage(err)
		}
		if request.IdempotencyKey == "" {
			request.IdempotencyKey = uuid.NewString()
		}
		request.StartPolicy = config.Policy
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		response, err := api.CreateRollout(cmd.Context(), request)
		if err != nil {
			return err
		}
		rollout := response.GetRollout()
		if detach {
			return printRollout(cmd, options, rollout)
		}
		until := untilTerminal
		if config.Policy == rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_MANUAL {
			until = untilReady
		}
		result, err := wait(cmd, options, api, rollout.GetID(), until)
		if err != nil {
			return err
		}
		rollout = result.GetRollout()
		if err := printWaitResult(cmd, options, rollout); err != nil {
			return err
		}
		return managedrollout.Outcome(result)
	}}
	cmd.Flags().StringVarP(&file, "file", "f", "", "path to an axrun/v1 Rollout spec")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "stable submission idempotency key")
	if config.AllowDetach {
		cmd.Flags().BoolVar(&detach, "detach", false, "return after durable acceptance")
	}
	return cmd
}

func plan(options *command.Options) *cobra.Command {
	return submit(options, submitConfig{
		Policy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_MANUAL,
		Use:    "plan",
		Short:  "Freeze and preflight a rollout until READY",
	})
}

func run(options *command.Options) *cobra.Command {
	return submit(options, submitConfig{
		Policy:      rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO,
		Use:         "run",
		Short:       "Create and run a rollout to terminal status",
		AllowDetach: true,
	})
}

func start(options *command.Options) *cobra.Command {
	var key string
	var detach bool
	cmd := &cobra.Command{Use: "start <ready-rollout-id>", Short: "Start an already frozen READY rollout", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if key == "" {
			key = uuid.NewString()
		}
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		response, err := api.StartRollout(cmd.Context(), &rolloutv1.StartRolloutRequest{
			RolloutID:      args[0],
			IdempotencyKey: key,
		})
		if err != nil {
			return err
		}
		if detach {
			return printRollout(cmd, options, response.GetRollout())
		}
		result, err := wait(cmd, options, api, args[0], untilTerminal)
		if err != nil {
			return err
		}
		rollout := result.GetRollout()
		if err := printWaitResult(cmd, options, rollout); err != nil {
			return err
		}
		return managedrollout.Outcome(result)
	}}
	cmd.Flags().StringVar(&key, "idempotency-key", "", "stable start idempotency key")
	cmd.Flags().BoolVar(&detach, "detach", false, "return after durable start acceptance")
	return cmd
}

func get(options *command.Options) *cobra.Command {
	return &cobra.Command{Use: "get <rollout-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		response, err := api.GetRollout(cmd.Context(), &rolloutv1.GetRolloutRequest{RolloutID: args[0]})
		if err != nil {
			return err
		}
		return command.PrintValue(cmd.OutOrStdout(), options.Output, response, "rollout=%s status=%s tasks=%d episodes=%d\n", response.GetRollout().GetID(), response.GetRollout().GetStatus(), response.GetRollout().GetSummary().GetTaskCount(), len(response.GetEpisodes()))
	}}
}

func list(options *command.Options) *cobra.Command {
	var namespace, taskSet, agent, model, cursor string
	var page int32
	var statusValues []string
	var labels map[string]string
	cmd := &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		statuses := make([]rolloutv1.RolloutStatus, 0, len(statusValues))
		for _, value := range statusValues {
			number, ok := rolloutv1.RolloutStatus_value["ROLLOUT_STATUS_"+strings.ToUpper(strings.TrimSpace(value))]
			if !ok {
				return command.Usage(fmt.Errorf("unknown rollout status %q", value))
			}
			statuses = append(statuses, rolloutv1.RolloutStatus(number))
		}
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		response, err := api.ListRollouts(cmd.Context(), &rolloutv1.ListRolloutsRequest{
			Filter: &rolloutv1.RolloutListFilter{
				Namespace:     namespace,
				TaskSetDigest: taskSet,
				Agent:         agent,
				Model:         model,
				Labels:        labels,
				Statuses:      statuses,
				Cursor:        cursor,
				PageSize:      page,
			},
		})
		if err != nil {
			return err
		}
		if options.Output == "json" {
			return command.PrintValue(cmd.OutOrStdout(), "json", response, "")
		}
		for _, rollout := range response.GetRollouts() {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%d/%d\n", rollout.GetID(), rollout.GetStatus(), rollout.GetSpec().GetAgent().GetName(), rollout.GetSpec().GetModel(), rollout.GetSummary().GetCompletedEpisodes()+rollout.GetSummary().GetFailedEpisodes()+rollout.GetSummary().GetCancelledEpisodes(), rollout.GetSummary().GetEpisodeCount())
		}
		return nil
	}}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "rollout namespace")
	cmd.Flags().StringSliceVar(&statusValues, "status", nil, "status filter (repeat or comma-separate)")
	cmd.Flags().StringVar(&taskSet, "task-set", "", "TaskSet descriptor digest or immutable reference")
	cmd.Flags().StringVar(&agent, "agent", "", "agent filter")
	cmd.Flags().StringVar(&model, "model", "", "model filter")
	cmd.Flags().StringToStringVar(&labels, "label", nil, "label equality filter (key=value)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	cmd.Flags().Int32Var(&page, "page-size", 50, "page size")
	return cmd
}

func watchCommand(options *command.Options) *cobra.Command {
	var until string
	cmd := &cobra.Command{Use: "watch <rollout-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		mode := untilMode(until)
		if mode != untilReady && mode != untilTerminal {
			return command.Usage(fmt.Errorf("--until must be ready or terminal"))
		}
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		result, err := wait(cmd, options, api, args[0], mode)
		if err != nil {
			return err
		}
		if err := printWaitResult(cmd, options, result.GetRollout()); err != nil {
			return err
		}
		return managedrollout.Outcome(result)
	}}
	cmd.Flags().StringVar(&until, "until", "terminal", "wait condition: ready or terminal")
	return cmd
}

func wait(cmd *cobra.Command, options *command.Options, api rolloutv1.RolloutControlClient, id string, until untilMode) (*rolloutv1.GetRolloutResponse, error) {
	mode := managedrollout.UntilTerminal
	if until == untilReady {
		mode = managedrollout.UntilReady
	}
	result, err := (managedrollout.Waiter{Client: api, OnEvent: func(event *rolloutv1.RolloutEvent) error {
		if options.Output == "json" {
			return command.PrintJSONLine(cmd.OutOrStdout(), event)
		}
		_, printErr := fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\t%s\t%s\n", event.GetSequence(), event.GetType(), event.GetPhase(), event.GetMessage())
		return printErr
	}}).Wait(cmd.Context(), id, mode)
	if err != nil && errors.Is(err, context.Canceled) {
		fmt.Fprintf(cmd.ErrOrStderr(), "rollout %s remains active; detached\n", id)
		return nil, command.Exit(130, fmt.Errorf("watch interrupted"))
	}
	return result, err
}

func inspect(options *command.Options) *cobra.Command {
	return &cobra.Command{Use: "inspect <rollout-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		response, err := api.DiagnoseRollout(cmd.Context(), &rolloutv1.DiagnoseRolloutRequest{RolloutID: args[0]})
		if err != nil {
			return err
		}
		return command.PrintValue(cmd.OutOrStdout(), options.Output, response, "rollout=%s status=%s diagnosis=%s\n%s\n", response.GetRollout().GetID(), response.GetRollout().GetStatus(), response.GetDiagnosis(), response.GetRecommendedAction())
	}}
}

func cancel(options *command.Options) *cobra.Command {
	return mutate(options, "cancel", func(api rolloutv1.RolloutControlClient, cmd *cobra.Command, id string) (*rolloutv1.Rollout, error) {
		r, e := api.CancelRollout(cmd.Context(), &rolloutv1.CancelRolloutRequest{RolloutID: id})
		return r.GetRollout(), e
	})
}

func retry(options *command.Options) *cobra.Command {
	return mutate(options, "retry", func(api rolloutv1.RolloutControlClient, cmd *cobra.Command, id string) (*rolloutv1.Rollout, error) {
		r, e := api.RetryRollout(cmd.Context(), &rolloutv1.RetryRolloutRequest{RolloutID: id})
		return r.GetRollout(), e
	})
}

func deleteRollout(options *command.Options) *cobra.Command {
	return mutate(options, "delete", func(api rolloutv1.RolloutControlClient, cmd *cobra.Command, id string) (*rolloutv1.Rollout, error) {
		r, e := api.DeleteRollout(cmd.Context(), &rolloutv1.DeleteRolloutRequest{RolloutID: id})
		return r.GetRollout(), e
	})
}

func mutate(options *command.Options, name string, call func(rolloutv1.RolloutControlClient, *cobra.Command, string) (*rolloutv1.Rollout, error)) *cobra.Command {
	return &cobra.Command{Use: name + " <rollout-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		rollout, err := call(api, cmd, args[0])
		if err != nil {
			return err
		}
		return printRollout(cmd, options, rollout)
	}}
}

func artifact(options *command.Options) *cobra.Command {
	root := &cobra.Command{Use: "artifact", Short: "List and download rollout evidence"}
	root.AddCommand(artifactList(options), artifactDownload(options), artifactDownloadAll(options))
	return root
}

func artifactDownload(options *command.Options) *cobra.Command {
	var output string
	var force bool
	cmd := &cobra.Command{Use: "download <artifact-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(output) == "" {
			return command.Usage(fmt.Errorf("--output is required"))
		}
		sdk, err := options.ControlClient(cmd.Context())
		if err != nil {
			return err
		}
		defer sdk.Close()
		downloader := managedrollout.NewDownloader(sdk.RolloutControl(), sdk.ArtifactData())
		artifact, err := downloader.Download(cmd.Context(), managedrollout.DownloadParams{
			ArtifactID:  args[0],
			Destination: output,
			Force:       force,
		})
		if err != nil {
			return err
		}
		return command.PrintValue(cmd.OutOrStdout(), options.Output, artifact, "%s\t%s\t%d\n", artifact.GetID(), output, artifact.GetSizeBytes())
	}}
	cmd.Flags().StringVar(&output, "output", "", "destination path")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing destination after integrity verification")
	return cmd
}

func artifactDownloadAll(options *command.Options) *cobra.Command {
	var outputDir string
	var force bool
	cmd := &cobra.Command{Use: "download-all <rollout-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(outputDir) == "" {
			return command.Usage(fmt.Errorf("--output-dir is required"))
		}
		var err error
		outputDir, err = managedrollout.PrepareOutputDirectory(outputDir)
		if err != nil {
			return err
		}
		sdk, err := options.ControlClient(cmd.Context())
		if err != nil {
			return err
		}
		defer sdk.Close()
		inventory, err := sdk.RolloutControl().ListArtifacts(cmd.Context(), &rolloutv1.ListArtifactsRequest{
			RolloutID: args[0],
		})
		if err != nil {
			return err
		}
		downloader := managedrollout.NewDownloader(sdk.RolloutControl(), sdk.ArtifactData())
		seen := map[string]struct{}{}
		for _, item := range inventory.GetArtifacts() {
			name := managedrollout.SafeName(item.GetID(), item.GetName())
			if _, exists := seen[name]; exists {
				return fmt.Errorf("artifact output collision for %q", name)
			}
			seen[name] = struct{}{}
			destination := filepath.Join(outputDir, name)
			if filepath.Dir(destination) != filepath.Clean(outputDir) {
				return fmt.Errorf("artifact output path escapes destination")
			}
			if _, err := downloader.Download(cmd.Context(), managedrollout.DownloadParams{
				ArtifactID:  item.GetID(),
				Destination: destination,
				Force:       force,
			}); err != nil {
				return err
			}
		}
		return command.PrintValue(cmd.OutOrStdout(), options.Output, inventory, "downloaded=%d dir=%s\n", len(inventory.GetArtifacts()), outputDir)
	}}
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "destination directory")
	cmd.Flags().BoolVar(&force, "force", false, "replace existing destinations after integrity verification")
	return cmd
}

func artifactList(options *command.Options) *cobra.Command {
	var episode string
	cmd := &cobra.Command{Use: "list <rollout-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		response, err := api.ListArtifacts(cmd.Context(), &rolloutv1.ListArtifactsRequest{
			RolloutID: args[0],
			EpisodeID: episode,
		})
		if err != nil {
			return err
		}
		if options.Output == "json" {
			return command.PrintValue(cmd.OutOrStdout(), "json", response, "")
		}
		for _, a := range response.GetArtifacts() {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\t%s\n", a.GetID(), a.GetName(), a.GetSizeBytes(), a.GetDigest())
		}
		return nil
	}}
	cmd.Flags().StringVar(&episode, "episode", "", "episode ID filter")
	return cmd
}

func compare(options *command.Options) *cobra.Command {
	return &cobra.Command{Use: "compare <rollout-id> <rollout-id> [rollout-id...]", Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 || len(args) > 5 {
			return command.Usage(fmt.Errorf("compare requires two to five rollout IDs"))
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		api, closer, err := client(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		response, err := api.CompareRollouts(cmd.Context(), &rolloutv1.CompareRolloutsRequest{RolloutIds: args})
		if err != nil {
			return err
		}
		return command.PrintValue(cmd.OutOrStdout(), options.Output, response, "compared=%d tasks=%d\n", len(response.GetRollouts()), len(response.GetTasks()))
	}}
}

func printRollout(cmd *cobra.Command, options *command.Options, rollout *rolloutv1.Rollout) error {
	return command.PrintValue(cmd.OutOrStdout(), options.Output, rollout, "rollout=%s status=%s\n", rollout.GetID(), rollout.GetStatus())
}

func printWaitResult(cmd *cobra.Command, options *command.Options, rollout *rolloutv1.Rollout) error {
	if options.Output == "json" {
		return command.PrintJSONLine(cmd.OutOrStdout(), rollout)
	}
	return printRollout(cmd, options, rollout)
}
