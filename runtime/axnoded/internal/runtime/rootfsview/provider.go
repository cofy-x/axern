package rootfsview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sirupsen/logrus"
)

const writableRootfsViewDir = "rootfs-views"

var mountInfoOctalEscapePattern = regexp.MustCompile(`\\[0-7]{3}`)

// View is a prepared container rootfs view. Writable views are active
// container snapshots and must be removed when the container is deleted.
type View struct {
	RootDir  string
	Writable bool
}

type Source struct {
	RootDir  string
	Readonly bool
}

// Provider owns the lifecycle of active writable rootfs views.
type Provider interface {
	Prepare(ctx context.Context, containerID string, source Source) (View, error)
	Remove(ctx context.Context, containerID string) error
}

type overlayProvider struct {
	filestoreDir string
}

type overlayView struct {
	LowerDirs []string
	UpperDir  string
	WorkDir   string
	MergedDir string
}

func NewOverlayProvider(filestoreDir string) Provider {
	return &overlayProvider{filestoreDir: filestoreDir}
}

func (p *overlayProvider) Prepare(_ context.Context, containerID string, source Source) (View, error) {
	if source.Readonly || source.RootDir == "" {
		return View{}, nil
	}

	rootPathReadonly, err := rootfsPathReadOnly(source.RootDir)
	if err != nil {
		return View{}, fmt.Errorf("detect rootfs mount mode for %s: %w", source.RootDir, err)
	}
	if !rootPathReadonly {
		return View{}, nil
	}
	if p.filestoreDir == "" {
		return View{}, fmt.Errorf("writable rootfs view requires runtime filestore_dir when rootfs path is read-only: %s", source.RootDir)
	}

	lowerDirs, err := resolveOverlayLowerDirs(source.RootDir)
	if err != nil {
		return View{}, err
	}
	view := overlayViewForContainer(containerID, p.filestoreDir, lowerDirs)
	if err := resetOverlayView(view); err != nil {
		return View{}, err
	}
	if err := mountOverlayView(view); err != nil {
		_ = cleanupOverlayView(filepath.Dir(view.MergedDir))
		return View{}, err
	}

	logrus.WithFields(logrus.Fields{
		"container_id":   containerID,
		"lowerdir_count": len(view.LowerDirs),
		"rootfs":         view.MergedDir,
		"storage":        p.filestoreDir,
	}).Debug("prepared writable rootfs view")
	return View{RootDir: view.MergedDir, Writable: true}, nil
}

func (p *overlayProvider) Remove(_ context.Context, containerID string) error {
	if containerID == "" || p.filestoreDir == "" {
		return nil
	}
	return cleanupOverlayView(filepath.Join(p.filestoreDir, writableRootfsViewDir, containerID))
}

func cleanupOverlayView(rootfsRoot string) error {
	if err := unmountOverlayView(overlayView{MergedDir: filepath.Join(rootfsRoot, "merged")}); err != nil {
		return err
	}
	return os.RemoveAll(rootfsRoot)
}

func overlayViewForContainer(containerID, filestoreDir string, lowerDirs []string) overlayView {
	rootfsRoot := filepath.Join(filestoreDir, writableRootfsViewDir, containerID)
	return overlayView{
		LowerDirs: append([]string(nil), lowerDirs...),
		UpperDir:  filepath.Join(rootfsRoot, "upper"),
		WorkDir:   filepath.Join(rootfsRoot, "work"),
		MergedDir: filepath.Join(rootfsRoot, "merged"),
	}
}

func resetOverlayView(rootfs overlayView) error {
	rootfsRoot := filepath.Dir(rootfs.MergedDir)
	if err := cleanupOverlayView(rootfsRoot); err != nil {
		return err
	}
	if err := os.RemoveAll(rootfsRoot); err != nil {
		return fmt.Errorf("reset writable rootfs view: %w", err)
	}
	for _, dir := range []string{rootfs.UpperDir, rootfs.WorkDir, rootfs.MergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir writable rootfs view dir %s: %w", dir, err)
		}
	}
	return nil
}
