package worker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/application/managedworker"
	"github.com/cofy-x/axern/apps/axrun/internal/command"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
	"github.com/spf13/cobra"
)

func Command(options *command.Options) *cobra.Command {
	var token, workerID, outputDir, executionContextName string
	var concurrency int
	cmd := &cobra.Command{Use: "worker", Short: "Run a durable Axrun rollout worker", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		controlContext, err := options.ResolveContext()
		if err != nil {
			return err
		}
		if controlContext == nil {
			return command.Usage(fmt.Errorf("an Axern control context is required"))
		}
		if executionContextName == "" {
			return command.Usage(fmt.Errorf("--execution-context is required"))
		}
		_, executionContext, found, err := clientconfig.Resolve(options.ConfigPath, executionContextName)
		if err != nil {
			return err
		}
		if !found || executionContext == nil {
			return command.Usage(fmt.Errorf("Axern execution context %q is required", executionContextName))
		}
		estimator, err := testPricingEstimator(os.Getenv("AXRUN_TEST_MODEL_PRICING_JSON"))
		if err != nil {
			return fmt.Errorf("parse test model pricing: %w", err)
		}
		return managedworker.Run(cmd.Context(), managedworker.Config{
			ControlContext:   controlContext,
			ExecutionContext: executionContext,
			BootstrapToken:   token,
			WorkerID:         workerID,
			OutputDir:        outputDir,
			Concurrency:      concurrency,
			EstimateCost:     estimator,
		})
	}}
	cmd.Flags().StringVar(&token, "token", os.Getenv("AXRUN_WORKER_TOKEN"), "controld rollout worker bootstrap token")
	cmd.Flags().StringVar(&workerID, "worker-id", "", "stable worker identity")
	cmd.Flags().StringVar(&outputDir, "output-dir", ".axrun/worker", "local execution evidence directory")
	cmd.Flags().StringVar(&executionContextName, "execution-context", "", "Axern context used for gateway-routed sandbox execution")
	cmd.Flags().IntVarP(&concurrency, "concurrency", "n", 4, "maximum concurrent work items")
	return cmd
}

type testModelPrice struct {
	InputPerMillion  float64 `json:"input_per_million"`
	OutputPerMillion float64 `json:"output_per_million"`
}

// testPricingEstimator is deliberately reachable only through the local test
// environment variable. It is not a product flag, provider contract, Helm
// value, or production price table entry.
func testPricingEstimator(raw string) (func(string, *domain.UsageMetrics) *domain.CostMetrics, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	prices := map[string]testModelPrice{}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&prices); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("pricing must contain exactly one JSON object")
	}
	return func(model string, usage *domain.UsageMetrics) *domain.CostMetrics {
		price, ok := prices[strings.ToLower(strings.TrimSpace(model))]
		if !ok || usage == nil || usage.InputTokens+usage.OutputTokens == 0 {
			return nil
		}
		amount := (float64(usage.InputTokens)*price.InputPerMillion + float64(usage.OutputTokens)*price.OutputPerMillion) / 1_000_000
		if amount <= 0 {
			return nil
		}
		return &domain.CostMetrics{Amount: amount, Currency: "USD"}
	}, nil
}
