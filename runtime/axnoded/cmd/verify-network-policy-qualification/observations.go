package main

import (
	"encoding/json"
	"os"
)

// Preserve ordered measurements without policy values or allocation identities.
// This sidecar is diagnostic evidence, not an alternative qualification report.
type workloadObservations struct {
	Method    string             `json:"method"`
	Complete  bool               `json:"complete"`
	DNS       []float64          `json:"dnsMilliseconds"`
	Prepare   []float64          `json:"prepareMilliseconds"`
	RuleScale []ruleObservations `json:"ruleScale"`
}

type ruleObservations struct {
	Rules     uint32    `json:"rules"`
	Prepare   []float64 `json:"prepareMilliseconds"`
	Reconcile []float64 `json:"reconcileMilliseconds"`
}

func writeWorkloadObservations(path string, observations workloadObservations) error {
	data, err := json.MarshalIndent(observations, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
