package rollout

import (
	"context"
	"time"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
)

type ArtifactUpload struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}
type ArtifactStore interface {
	PresignUpload(context.Context, string, string, int64, string, time.Duration) (ArtifactUpload, error)
	Verify(context.Context, string, int64, string) error
	PresignDownload(context.Context, string, time.Duration) (string, time.Time, error)
	DeletePrefix(context.Context, string) error
}
type ArtifactReader interface {
	ListArtifacts(context.Context, string, string) ([]*rolloutv1.Artifact, error)
	PrepareArtifactDownload(context.Context, string, time.Duration) (*rolloutv1.Artifact, string, time.Time, bool, error)
	ResolveArtifactDownload(context.Context, string, int64, time.Duration) (*rolloutv1.Artifact, string, map[string]string, time.Time, error)
}
