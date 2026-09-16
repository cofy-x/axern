package localstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestWriteArtifactManifestPublishesOnce(t *testing.T) {
	artifactDir := filepath.Join(t.TempDir(), "runs", "run", "episodes", "episode", "artifacts")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := New(filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(artifactDir)))))
	first := domain.ArtifactManifest{EpisodeID: "episode", GeneratedAt: time.Now().UTC()}
	if _, err := store.WriteArtifactManifest(artifactDir, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.EpisodeID = "replacement"
	if _, err := store.WriteArtifactManifest(artifactDir, second); err == nil {
		t.Fatal("second manifest publication unexpectedly replaced committed evidence")
	}
	got, err := readJSONFile[domain.ArtifactManifest](filepath.Join(artifactDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.EpisodeID != "episode" {
		t.Fatalf("manifest episode_id = %q", got.EpisodeID)
	}
}
