package exportdata

import "github.com/cofy-x/axern/apps/axrun/internal/domain"

type episodeBundle struct {
	RunRoot  string
	Run      domain.RolloutRun
	Plan     domain.RolloutPlan
	Task     domain.TaskInstance
	Episode  domain.Episode
	Agent    domain.AgentResult
	Verifier domain.VerifierResult
	Reward   domain.Reward
	Refs     EpisodeRefs
}
