package managedworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
)

type evidenceEntry struct {
	ArtifactID string `json:"artifact_id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	MediaType  string `json:"media_type"`
	SizeBytes  int64  `json:"size_bytes"`
	Digest     string `json:"digest"`
}

type artifactUploadRequest struct {
	Kind      string
	Name      string
	MediaType string
	Data      []byte
}

func (w Worker) uploadEvidence(ctx context.Context, work *workerrolloutv1.WorkItem, leaseToken string, result approllout.Result) (string, error) {
	if len(result.Episodes) != 1 {
		return "", fmt.Errorf("controlled episode has %d evidence layouts", len(result.Episodes))
	}
	episode := result.Episodes[0]
	files := []struct{ kind, path, mediaType string }{
		{"task", episode.TaskJSONPath, "application/json"},
		{"episode", episode.EpisodeJSONPath, "application/json"},
		{"trajectory", episode.TrajectoryPath, "application/x-ndjson"},
		{"agent", episode.AgentJSONPath, "application/json"},
		{"verifier", episode.VerifierJSONPath, "application/json"},
		{"reward", episode.RewardJSONPath, "application/json"},
	}
	entries := make([]evidenceEntry, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return "", fmt.Errorf("read %s evidence: %w", file.kind, err)
		}
		artifact, err := w.uploadArtifact(ctx, work, leaseToken, artifactUploadRequest{
			Kind:      file.kind,
			Name:      filepath.Base(file.path),
			MediaType: file.mediaType,
			Data:      data,
		})
		if err != nil {
			return "", err
		}
		entries = append(entries, evidenceEntry{
			ArtifactID: artifact.GetID(),
			Kind:       artifact.GetKind(),
			Name:       artifact.GetName(),
			MediaType:  artifact.GetMediaType(),
			SizeBytes:  artifact.GetSizeBytes(),
			Digest:     artifact.GetDigest(),
		})
	}
	manifest, err := json.Marshal(struct {
		Version             int             `json:"version"`
		RolloutID           string          `json:"rollout_id"`
		EpisodeID           string          `json:"episode_id"`
		ExecutionGeneration int64           `json:"execution_generation"`
		Artifacts           []evidenceEntry `json:"artifacts"`
	}{
		Version:             1,
		RolloutID:           work.GetRolloutID(),
		EpisodeID:           work.GetEpisodeID(),
		ExecutionGeneration: work.GetExecutionGeneration(),
		Artifacts:           entries,
	})
	if err != nil {
		return "", err
	}
	artifact, err := w.uploadArtifact(ctx, work, leaseToken, artifactUploadRequest{
		Kind:      "manifest",
		Name:      "evidence-manifest.json",
		MediaType: "application/json",
		Data:      manifest,
	})
	if err != nil {
		return "", err
	}
	return artifact.GetID(), nil
}

func (w Worker) uploadArtifact(ctx context.Context, work *workerrolloutv1.WorkItem, leaseToken string, request artifactUploadRequest) (*rolloutv1.Artifact, error) {
	sum := sha256.Sum256(request.Data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	upload, err := w.client.CreateArtifactUpload(ctx, &workerrolloutv1.CreateArtifactUploadRequest{
		WorkID:     work.GetID(),
		LeaseToken: leaseToken,
		Kind:       request.Kind,
		Name:       request.Name,
		MediaType:  request.MediaType,
		SizeBytes:  int64(len(request.Data)),
		Digest:     digest,
	})
	if err != nil {
		return nil, fmt.Errorf("create %s artifact upload: %w", request.Kind, err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPut, upload.GetUploadUrl(), bytes.NewReader(request.Data))
	if err != nil {
		return nil, err
	}
	for key, value := range upload.GetHeaders() {
		httpRequest.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("upload %s artifact: %w", request.Kind, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upload %s artifact: object store returned %s", request.Kind, response.Status)
	}
	committed, err := w.client.CommitArtifact(ctx, &workerrolloutv1.CommitArtifactRequest{
		WorkID:     work.GetID(),
		LeaseToken: leaseToken,
		ArtifactID: upload.GetArtifact().GetID(),
	})
	if err != nil {
		return nil, fmt.Errorf("commit %s artifact: %w", request.Kind, err)
	}
	return committed.GetArtifact(), nil
}
