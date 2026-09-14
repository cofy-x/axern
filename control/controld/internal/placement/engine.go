package placement

import (
	"sort"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
)

const (
	defaultHeartbeatFreshnessWindow = 15 * time.Second
	defaultSummaryFreshnessWindow   = 15 * time.Second
)

type Config struct {
	HeartbeatFreshnessWindow time.Duration
	SummaryFreshnessWindow   time.Duration
	ResourcePolicy           resourcekernel.AdmissionPolicy
}

type Engine struct {
	heartbeatFreshnessWindow time.Duration
	summaryFreshnessWindow   time.Duration
	resourcePolicy           resourcekernel.AdmissionPolicy
}

func NewEngine(cfg Config) *Engine {
	if cfg.HeartbeatFreshnessWindow <= 0 {
		cfg.HeartbeatFreshnessWindow = defaultHeartbeatFreshnessWindow
	}
	if cfg.SummaryFreshnessWindow <= 0 {
		cfg.SummaryFreshnessWindow = defaultSummaryFreshnessWindow
	}
	cfg.ResourcePolicy = resourcekernel.NormalizeAdmissionPolicy(cfg.ResourcePolicy)
	return &Engine{
		heartbeatFreshnessWindow: cfg.HeartbeatFreshnessWindow,
		summaryFreshnessWindow:   cfg.SummaryFreshnessWindow,
		resourcePolicy:           cfg.ResourcePolicy,
	}
}

func (e *Engine) Plan(snapshot nodekernel.Snapshot, req *placementkernel.Request, now time.Time) ([]*placementkernel.Evaluation, []*placementkernel.Evaluation) {
	planned := make([]*placementkernel.Evaluation, 0, len(snapshot.Records))
	for _, record := range snapshot.Records {
		candidate := e.evaluateCandidate(CandidateInput{
			Record:  record,
			Request: req,
			Now:     now,
		})
		if candidate != nil {
			planned = append(planned, candidate)
		}
	}

	eligible := make([]*placementkernel.Evaluation, 0, len(planned))
	rejected := make([]*placementkernel.Evaluation, 0, len(planned))
	for _, candidate := range planned {
		if candidate.GetState() == placementkernel.CandidateStateEligible {
			eligible = append(eligible, candidate)
			continue
		}
		rejected = append(rejected, candidate)
	}

	sort.Slice(eligible, func(i, j int) bool {
		return compareEligibleCandidates(eligible[i], eligible[j])
	})
	sort.Slice(rejected, func(i, j int) bool {
		return rejected[i].GetNodeID() < rejected[j].GetNodeID()
	})
	return eligible, rejected
}

func (e *Engine) Evaluate(record *nodekernel.Record, req *placementkernel.Request, now time.Time) *placementkernel.Evaluation {
	return e.evaluateCandidate(CandidateInput{Record: record, Request: req, Now: now})
}
