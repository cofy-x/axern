package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/reward"
)

func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

type fakeBackend struct {
	status domain.EpisodeStatus
}

type providerPreflightBackend struct {
	profileCalls int
	probeCalls   int
	probeErr     error
}

func (b *providerPreflightBackend) Preflight() error { return nil }

func (b *providerPreflightBackend) PreflightProviderProfile(domain.AgentSpec) error {
	b.profileCalls++
	return nil
}

func (b *providerPreflightBackend) PreflightProvider(context.Context, domain.AgentSpec, domain.ModelSpec) error {
	b.probeCalls++
	return b.probeErr
}

func (b *providerPreflightBackend) Execute(request backend.ExecuteRequest) (domain.Episode, error) {
	return fakeBackend{status: domain.EpisodeStatusCompleted}.Execute(request)
}

func (f fakeBackend) Preflight() error {
	return nil
}

type taskStatusBackend struct {
	statuses map[string]domain.EpisodeStatus
}

func (b taskStatusBackend) Preflight() error {
	return nil
}

func (b taskStatusBackend) Execute(request backend.ExecuteRequest) (domain.Episode, error) {
	episode := request.Episode
	status, ok := b.statuses[request.Task.ID]
	if !ok {
		status = domain.EpisodeStatusCompleted
	}
	episode.Status = status
	if status == domain.EpisodeStatusFailed {
		episode.FailureClass = domain.FailureClassVerifierFailed
	}
	finalizeTestEpisode(&episode)
	if err := writeFakeOutcome(request, status); err != nil {
		return episode, err
	}
	if err := writeFakeManifest(request, &episode); err != nil {
		return episode, err
	}
	if err := request.Store.WriteEpisode(request.Paths.EpisodeJSONPath, episode); err != nil {
		return episode, err
	}
	return episode, nil
}

type failingTaskBackend struct {
	taskIDs map[string]struct{}
}

func (b failingTaskBackend) Preflight() error {
	return nil
}

func (b failingTaskBackend) Execute(request backend.ExecuteRequest) (domain.Episode, error) {
	if _, ok := b.taskIDs[request.Task.ID]; ok {
		return request.Episode, fmt.Errorf("boom")
	}
	episode := request.Episode
	episode.Status = domain.EpisodeStatusCompleted
	finalizeTestEpisode(&episode)
	if err := writeFakeOutcome(request, episode.Status); err != nil {
		return episode, err
	}
	if err := writeFakeManifest(request, &episode); err != nil {
		return episode, err
	}
	if err := request.Store.WriteEpisode(request.Paths.EpisodeJSONPath, episode); err != nil {
		return episode, err
	}
	return episode, nil
}

type invalidArtifactBackend struct{}

func (b invalidArtifactBackend) Preflight() error {
	return nil
}

func (b invalidArtifactBackend) Execute(request backend.ExecuteRequest) (domain.Episode, error) {
	episode := request.Episode
	episode.Status = domain.EpisodeStatusCompleted
	finalizeTestEpisode(&episode)
	if err := request.Store.WriteAgentResult(request.Paths.AgentJSONPath, domain.AgentResult{
		Status: domain.AgentStatusCompleted,
		Artifacts: []domain.ArtifactRef{
			{Path: "/tmp/outside", Kind: domain.ArtifactKindAgentRawLog},
		},
	}); err != nil {
		return episode, err
	}
	if err := writeFakeVerifierAndReward(request, episode.Status); err != nil {
		return episode, err
	}
	if err := writeFakeManifest(request, &episode); err != nil {
		return episode, err
	}
	if err := request.Store.WriteEpisode(request.Paths.EpisodeJSONPath, episode); err != nil {
		return episode, err
	}
	return episode, nil
}

type trackingBackend struct {
	delay   time.Duration
	enter   chan string
	release chan struct{}
}

func (b trackingBackend) Preflight() error {
	return nil
}

func (b trackingBackend) Execute(request backend.ExecuteRequest) (domain.Episode, error) {
	b.enter <- request.Task.ID
	if b.release != nil {
		<-b.release
	} else if b.delay > 0 {
		time.Sleep(b.delay)
	}
	episode := request.Episode
	episode.Status = domain.EpisodeStatusCompleted
	finalizeTestEpisode(&episode)
	if err := writeFakeOutcome(request, episode.Status); err != nil {
		return episode, err
	}
	if err := writeFakeManifest(request, &episode); err != nil {
		return episode, err
	}
	if err := request.Store.WriteEpisode(request.Paths.EpisodeJSONPath, episode); err != nil {
		return episode, err
	}
	return episode, nil
}

type barrierFailingBackend struct {
	enter   chan string
	release chan struct{}
}

func (b barrierFailingBackend) Preflight() error {
	return nil
}

func (b barrierFailingBackend) Execute(request backend.ExecuteRequest) (domain.Episode, error) {
	b.enter <- request.Task.ID
	<-b.release
	return request.Episode, fmt.Errorf("boom")
}

type zeroEpisodeFailingBackend struct{}

func (b zeroEpisodeFailingBackend) Preflight() error {
	return nil
}

func (b zeroEpisodeFailingBackend) Execute(backend.ExecuteRequest) (domain.Episode, error) {
	return domain.Episode{}, fmt.Errorf("boom")
}

func (f fakeBackend) Execute(request backend.ExecuteRequest) (domain.Episode, error) {
	episode := request.Episode
	episode.Status = f.status
	if f.status != domain.EpisodeStatusPending {
		finalizeTestEpisode(&episode)
		if err := writeFakeOutcome(request, f.status); err != nil {
			return episode, err
		}
		if err := writeFakeManifest(request, &episode); err != nil {
			return episode, err
		}
	}
	if err := request.Store.WriteEpisode(request.Paths.EpisodeJSONPath, episode); err != nil {
		return episode, err
	}
	return episode, nil
}

func finalizeTestEpisode(episode *domain.Episode) {
	now := time.Now().UTC()
	if episode.StartedAt == nil {
		episode.StartedAt = &now
	}
	episode.FinishedAt = &now
	episode.CompletedAt = &now
	episode.DurationMS = episode.FinishedAt.Sub(*episode.StartedAt).Milliseconds()
}

func writeFakeOutcome(request backend.ExecuteRequest, status domain.EpisodeStatus) error {
	if err := request.Store.WriteAgentResult(request.Paths.AgentJSONPath, domain.AgentResult{
		Status:  domain.AgentStatusCompleted,
		Summary: "fake agent completed",
	}); err != nil {
		return err
	}
	return writeFakeVerifierAndReward(request, status)
}

func writeFakeManifest(request backend.ExecuteRequest, episode *domain.Episode) error {
	if episode == nil || episode.Status == domain.EpisodeStatusPending {
		return nil
	}
	path, err := request.Store.WriteArtifactManifest(request.Paths.ArtifactDir, domain.ArtifactManifest{
		SchemaVersion: domain.LocalSchemaVersion,
		EpisodeID:     episode.ID,
		GeneratedAt:   time.Now().UTC(),
		Entries:       []domain.ArtifactManifestEntry{},
	})
	if err != nil {
		return err
	}
	episode.ArtifactManifestPath = path
	return nil
}

func writeFakeVerifierAndReward(request backend.ExecuteRequest, status domain.EpisodeStatus) error {
	verifierStatus := domain.EpisodeStatusCompleted
	errorText := ""
	if status == domain.EpisodeStatusFailed {
		verifierStatus = domain.EpisodeStatusFailed
		errorText = "fake verifier failed"
	}
	verifier := domain.VerifierResult{
		Status: verifierStatus,
		Type:   request.Task.Verifier.Type,
		Error:  errorText,
	}
	if err := request.Store.WriteVerifierResult(request.Paths.VerifierJSONPath, verifier); err != nil {
		return err
	}
	return request.Store.WriteReward(request.Paths.RewardJSONPath, reward.Normalize(verifier))
}
