package domain

import "time"

type ArtifactManifestStatus string

const (
	ArtifactManifestStatusPresent ArtifactManifestStatus = "present"
	ArtifactManifestStatusMissing ArtifactManifestStatus = "missing"
	ArtifactManifestStatusFailed  ArtifactManifestStatus = "failed"
)

type ArtifactManifest struct {
	SchemaVersion string                  `json:"schema_version,omitempty"`
	EpisodeID     string                  `json:"episode_id"`
	GeneratedAt   time.Time               `json:"generated_at"`
	Entries       []ArtifactManifestEntry `json:"entries"`
}

type ArtifactManifestEntry struct {
	Kind        ArtifactKind           `json:"kind,omitempty"`
	Source      string                 `json:"source,omitempty"`
	Path        string                 `json:"path"`
	Status      ArtifactManifestStatus `json:"status"`
	SHA256      string                 `json:"sha256,omitempty"`
	MediaType   string                 `json:"media_type,omitempty"`
	SizeBytes   int64                  `json:"size_bytes,omitempty"`
	Description string                 `json:"description,omitempty"`
	Producer    string                 `json:"producer,omitempty"`
	Role        ArtifactRole           `json:"role,omitempty"`
	Error       string                 `json:"error,omitempty"`
}
