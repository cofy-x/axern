package rollout

import (
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestRunStatusForEpisodes(t *testing.T) {
	tests := map[string]struct {
		episodes []domain.Episode
		want     domain.RunStatus
	}{
		"completed": {
			episodes: []domain.Episode{{Status: domain.EpisodeStatusCompleted}},
			want:     domain.RunStatusCompleted,
		},
		"verifier failure is a completed rollout": {
			episodes: []domain.Episode{{Status: domain.EpisodeStatusFailed, FailureClass: domain.FailureClassVerifierFailed}},
			want:     domain.RunStatusCompleted,
		},
		"pending episode keeps rollout running": {
			episodes: []domain.Episode{{Status: domain.EpisodeStatusPending}},
			want:     domain.RunStatusRunning,
		},
		"recorded infrastructure failure fails rollout": {
			episodes: []domain.Episode{{Status: domain.EpisodeStatusFailed, FailureClass: domain.FailureClassInfrastructure}},
			want:     domain.RunStatusFailed,
		},
		"infrastructure failure takes precedence over pending work": {
			episodes: []domain.Episode{
				{Status: domain.EpisodeStatusPending},
				{Status: domain.EpisodeStatusFailed, FailureClass: domain.FailureClassInfrastructure},
			},
			want: domain.RunStatusFailed,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := runStatusForEpisodes(test.episodes); got != test.want {
				t.Fatalf("runStatusForEpisodes() = %q, want %q", got, test.want)
			}
		})
	}
}
