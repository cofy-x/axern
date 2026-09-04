package qualification

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

type Budget struct {
	SchemaVersion            int     `json:"schemaVersion"`
	MaxLatencyP95Ratio       float64 `json:"maxLatencyP95Ratio"`
	MaxLatencyP99Ratio       float64 `json:"maxLatencyP99Ratio"`
	MinThroughputRatio       float64 `json:"minThroughputRatio"`
	MaxRSSRatio              float64 `json:"maxRssRatio"`
	MinConcurrencyRatio      float64 `json:"minConcurrencyRatio"`
	MaxFailureRate           float64 `json:"maxFailureRate"`
	MaxRuleScaleLatencyRatio float64 `json:"maxRuleScaleLatencyRatio"`
	MaxRuleScaleRSSRatio     float64 `json:"maxRuleScaleRssRatio"`
}

type Violation struct {
	Scenario string  `json:"scenario"`
	Metric   string  `json:"metric"`
	Observed float64 `json:"observed"`
	Limit    float64 `json:"limit"`
}

type Comparison struct {
	SchemaVersion   int         `json:"schemaVersion"`
	BaselineCommit  string      `json:"baselineCommit"`
	CandidateCommit string      `json:"candidateCommit"`
	EnvironmentID   string      `json:"environmentId"`
	Comparable      bool        `json:"comparable"`
	Passed          bool        `json:"passed"`
	Violations      []Violation `json:"violations"`
}

func WriteComparison(writer io.Writer, comparison Comparison) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(comparison)
}

func DecodeBudget(reader io.Reader) (Budget, error) {
	var budget Budget
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&budget); err != nil {
		return Budget{}, fmt.Errorf("decode qualification budget: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Budget{}, err
	}
	if err := budget.Validate(); err != nil {
		return Budget{}, err
	}
	return budget, nil
}

func (budget Budget) Validate() error {
	if budget.SchemaVersion != SchemaVersion {
		return fmt.Errorf("budget schemaVersion = %d, want %d", budget.SchemaVersion, SchemaVersion)
	}
	if budget.MaxLatencyP95Ratio < 1 || budget.MaxLatencyP99Ratio < 1 || budget.MinThroughputRatio <= 0 || budget.MinThroughputRatio > 1 || budget.MaxRSSRatio < 1 || budget.MinConcurrencyRatio <= 0 || budget.MinConcurrencyRatio > 1 || budget.MaxFailureRate < 0 || budget.MaxFailureRate >= 1 || budget.MaxRuleScaleLatencyRatio < 1 || budget.MaxRuleScaleRSSRatio < 1 {
		return errors.New("qualification budget ratios are outside their safe ranges")
	}
	return nil
}

func Compare(baseline, candidate Report, budget Budget) (Comparison, error) {
	comparison := Comparison{
		SchemaVersion: SchemaVersion, BaselineCommit: baseline.Subject.Commit, CandidateCommit: candidate.Subject.Commit,
		EnvironmentID: candidate.Environment.EnvironmentID,
	}
	if err := baseline.Validate(true); err != nil {
		return comparison, fmt.Errorf("baseline: %w", err)
	}
	if err := candidate.Validate(true); err != nil {
		return comparison, fmt.Errorf("candidate: %w", err)
	}
	if err := budget.Validate(); err != nil {
		return comparison, err
	}
	if baseline.Environment.EnvironmentID != candidate.Environment.EnvironmentID {
		return comparison, errors.New("reports are not comparable: environment provenance differs")
	}
	if !EqualParameters(baseline.Parameters, candidate.Parameters) {
		return comparison, errors.New("reports are not comparable: qualification parameters differ")
	}
	comparison.Comparable = true
	baselineByKey := map[string]ScenarioResult{}
	for _, scenario := range baseline.Scenarios {
		baselineByKey[scenarioKey(scenario)] = scenario
	}
	for _, current := range candidate.Scenarios {
		key := scenarioKey(current)
		previous := baselineByKey[key]
		compareScenario(&comparison, key, previous.Metrics, current.Metrics, budget)
	}
	sort.Slice(comparison.Violations, func(i, j int) bool {
		if comparison.Violations[i].Scenario != comparison.Violations[j].Scenario {
			return comparison.Violations[i].Scenario < comparison.Violations[j].Scenario
		}
		return comparison.Violations[i].Metric < comparison.Violations[j].Metric
	})
	comparison.Passed = len(comparison.Violations) == 0
	return comparison, nil
}

func compareScenario(comparison *Comparison, key string, baseline, candidate ScenarioMetrics, budget Budget) {
	for name, pair := range map[string][2]*Distribution{
		"prepare_latency":             {baseline.PrepareLatencyMS, candidate.PrepareLatencyMS},
		"sandbox_start_latency":       {baseline.SandboxStartLatencyMS, candidate.SandboxStartLatencyMS},
		"dns_latency":                 {baseline.DNSLatencyMS, candidate.DNSLatencyMS},
		"first_connection_latency":    {baseline.FirstConnectionLatencyMS, candidate.FirstConnectionLatencyMS},
		"restart_convergence_latency": {baseline.RestartConvergenceLatencyMS, candidate.RestartConvergenceLatencyMS},
	} {
		compareDistribution(comparison, key, name, pair[0], pair[1], budget)
	}
	compareHigherBetter(comparison, key, "http_throughput", baseline.HTTPThroughputMbps, candidate.HTTPThroughputMbps, budget.MinThroughputRatio)
	compareHigherBetter(comparison, key, "tls_throughput", baseline.TLSThroughputMbps, candidate.TLSThroughputMbps, budget.MinThroughputRatio)
	compareRatio(comparison, key, "max_rss", float64(candidate.MaxRSSBytes), float64(baseline.MaxRSSBytes), budget.MaxRSSRatio)
	if baseline.PeakConcurrentSessions > 0 && float64(candidate.PeakConcurrentSessions)/float64(baseline.PeakConcurrentSessions) < budget.MinConcurrencyRatio {
		comparison.Violations = append(comparison.Violations, Violation{Scenario: key, Metric: "peak_concurrent_sessions_ratio", Observed: float64(candidate.PeakConcurrentSessions) / float64(baseline.PeakConcurrentSessions), Limit: budget.MinConcurrencyRatio})
	}
	failureRate := float64(candidate.Failures) / float64(candidate.Operations)
	if failureRate > budget.MaxFailureRate {
		comparison.Violations = append(comparison.Violations, Violation{Scenario: key, Metric: "failure_rate", Observed: failureRate, Limit: budget.MaxFailureRate})
	}
	baselineScale := map[uint32]RuleScalePoint{}
	for _, point := range baseline.RuleScale {
		baselineScale[point.Rules] = point
	}
	candidateScale := map[uint32]RuleScalePoint{}
	for _, point := range candidate.RuleScale {
		candidateScale[point.Rules] = point
		previous, exists := baselineScale[point.Rules]
		if !exists {
			comparison.Violations = append(comparison.Violations, Violation{Scenario: key, Metric: fmt.Sprintf("rule_scale_%d_missing_baseline", point.Rules), Observed: 1, Limit: 0})
			continue
		}
		compareRatio(comparison, key, fmt.Sprintf("rule_scale_%d_prepare", point.Rules), point.PrepareP95MS, previous.PrepareP95MS, budget.MaxRuleScaleLatencyRatio)
		compareRatio(comparison, key, fmt.Sprintf("rule_scale_%d_reconcile", point.Rules), point.ReconcileP95MS, previous.ReconcileP95MS, budget.MaxRuleScaleLatencyRatio)
		compareRatio(comparison, key, fmt.Sprintf("rule_scale_%d_rss", point.Rules), float64(point.RSSBytes), float64(previous.RSSBytes), budget.MaxRuleScaleRSSRatio)
	}
	for count := range baselineScale {
		if _, exists := candidateScale[count]; !exists {
			comparison.Violations = append(comparison.Violations, Violation{Scenario: key, Metric: fmt.Sprintf("rule_scale_%d_missing_candidate", count), Observed: 0, Limit: 1})
		}
	}
}

func compareDistribution(comparison *Comparison, scenario, name string, baseline, candidate *Distribution, budget Budget) {
	if baseline == nil || candidate == nil {
		if baseline != nil || candidate != nil {
			comparison.Violations = append(comparison.Violations, Violation{Scenario: scenario, Metric: name + "_availability", Observed: boolNumber(candidate != nil), Limit: boolNumber(baseline != nil)})
		}
		return
	}
	compareRatio(comparison, scenario, name+"_p95", candidate.P95, baseline.P95, budget.MaxLatencyP95Ratio)
	compareRatio(comparison, scenario, name+"_p99", candidate.P99, baseline.P99, budget.MaxLatencyP99Ratio)
}

func compareHigherBetter(comparison *Comparison, scenario, name string, baseline, candidate *float64, minimum float64) {
	if baseline == nil || candidate == nil {
		if baseline != nil || candidate != nil {
			comparison.Violations = append(comparison.Violations, Violation{Scenario: scenario, Metric: name + "_availability", Observed: boolNumber(candidate != nil), Limit: boolNumber(baseline != nil)})
		}
		return
	}
	ratio := *candidate / *baseline
	if ratio < minimum {
		comparison.Violations = append(comparison.Violations, Violation{Scenario: scenario, Metric: name + "_ratio", Observed: ratio, Limit: minimum})
	}
}

func compareRatio(comparison *Comparison, scenario, metric string, candidate, baseline, maximum float64) {
	if baseline == 0 {
		if candidate != 0 {
			comparison.Violations = append(comparison.Violations, Violation{Scenario: scenario, Metric: metric + "_ratio", Observed: candidate, Limit: 0})
		}
		return
	}
	ratio := candidate / baseline
	if ratio > maximum {
		comparison.Violations = append(comparison.Violations, Violation{Scenario: scenario, Metric: metric + "_ratio", Observed: ratio, Limit: maximum})
	}
}

func boolNumber(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
