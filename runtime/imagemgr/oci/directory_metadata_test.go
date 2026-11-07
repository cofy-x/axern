package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func metadataTestLayer(t *testing.T, headers ...*tar.Header) v1.Layer {
	t.Helper()
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	for _, header := range headers {
		copy := *header
		copy.Uid, copy.Gid = os.Getuid(), os.Getgid()
		if err := writer.WriteHeader(&copy); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return static.NewLayer(data.Bytes(), types.OCIUncompressedLayer)
}

func TestImplicitParentKeepsEffectiveLowerPermissions(t *testing.T) {
	mgr := newTestManager(t)
	layers := []v1.Layer{
		metadataTestLayer(t, &tar.Header{Name: "private", Typeflag: tar.TypeDir, Mode: 0700},
			&tar.Header{Name: "private/old", Typeflag: tar.TypeReg, Mode: 0644}),
		metadataTestLayer(t, &tar.Header{Name: "private/new", Typeflag: tar.TypeReg, Mode: 0644},
			&tar.Header{Name: "brand-new/new", Typeflag: tar.TypeReg, Mode: 0644}),
	}
	facts, err := mgr.extractLayersWithWorkers(context.Background(), layers)
	if err != nil {
		t.Fatal(err)
	}
	chains, err := mgr.ensureChainLowerDirs([]string{"base", "new-child"}, facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range chains {
		assertDirectoryMode(t, filepath.Join(root, "private"), 0700)
	}
	assertDirectoryMode(t, filepath.Join(chains[1], "brand-new"), 0755)
	assertDirectoryMode(t, filepath.Join(facts[1].Path, "private"), 0755)
}

func TestImplicitMetadataRespectsCurrentAndPreviousVisibility(t *testing.T) {
	for _, test := range []struct {
		name, barrier, namespace string
		wantMode                 os.FileMode
		wantError                bool
	}{
		{"inherit", "", "", 0700, false},
		{"previous root opaque", "previous-root", "trusted", 0755, false},
		{"previous ancestor opaque", "previous-parent", "trusted", 0755, false},
		{"current root opaque", "current-root", "trusted", 0755, false},
		{"current ancestor opaque", "current-parent", "trusted", 0755, false},
		{"current leaf opaque retains stat", "current-leaf", "trusted", 0700, false},
		{"previous leaf opaque retains stat", "base-leaf", "trusted", 0700, false},
		{"previous whiteout", "previous-leaf-whiteout", "trusted", 0755, false},
		{"previous ancestor whiteout", "previous-parent-whiteout", "trusted", 0755, false},
		{"previous non-directory", "previous-parent-file", "", 0, true},
		{"previous symlink", "previous-parent-symlink", "", 0, true},
		{"unqualified previous opaque", "previous-root", "user", 0, true},
		{"unqualified current opaque", "current-parent", "user", 0, true},
		{"unqualified whiteout", "previous-leaf-whiteout", "user", 0, true},
		{"unreadable visibility", "previous-root", "error", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, previous, current := t.TempDir(), t.TempDir(), t.TempDir()
			for _, root := range []string{base, current} {
				if err := os.MkdirAll(filepath.Join(root, "parent/private"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(filepath.Join(base, "parent/private"), 0700); err != nil {
				t.Fatal(err)
			}
			marker := ""
			switch test.barrier {
			case "previous-root":
				marker = previous
			case "previous-parent":
				marker = filepath.Join(previous, "parent")
			case "current-root":
				marker = current
			case "current-parent":
				marker = filepath.Join(current, "parent")
			case "current-leaf":
				marker = filepath.Join(current, "parent/private")
			case "base-leaf":
				marker = filepath.Join(base, "parent/private")
			case "previous-leaf-whiteout":
				marker = filepath.Join(previous, "parent/private")
			case "previous-parent-whiteout", "previous-parent-file", "previous-parent-symlink":
				marker = filepath.Join(previous, "parent")
			}
			if strings.Contains(test.barrier, "whiteout") || test.barrier == "previous-parent-file" {
				if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(marker, nil, 0644); err != nil {
					t.Fatal(err)
				}
			} else if test.barrier == "previous-parent-symlink" {
				if err := os.Symlink(base, marker); err != nil {
					t.Fatal(err)
				}
			} else if marker != "" {
				if err := os.MkdirAll(marker, 0755); err != nil {
					t.Fatal(err)
				}
			}
			// These qualified marker facts exercise the metadata resolver only;
			// they are not a kernel OverlayFS qualification or a userxattr mount.
			attributes := func(path, name string) ([]byte, bool, error) {
				if path != marker || test.namespace == "" {
					return nil, true, nil
				}
				if test.namespace == "error" {
					return nil, false, os.ErrPermission
				}
				want := "opaque"
				if strings.Contains(test.barrier, "whiteout") {
					want = "whiteout"
				}
				if name != want {
					return nil, true, nil
				}
				return []byte("y"), test.namespace == "trusted", nil
			}
			err := inheritImplicitDirectoryMetadata(current, []string{base, previous}, []string{"parent/private"}, attributes)
			if test.wantError {
				if err == nil {
					t.Fatal("uncertain visibility was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertDirectoryMode(t, filepath.Join(current, "parent/private"), test.wantMode)
		})
	}
}

func TestImplicitFactsTrackDirectoryReplacement(t *testing.T) {
	for _, test := range []struct {
		name string
		last *tar.Header
		want []string
	}{
		{"later explicit parent", &tar.Header{Name: "parent", Typeflag: tar.TypeDir, Mode: 0700}, []string{"parent/deep"}},
		{"replace subtree", &tar.Header{Name: "parent", Typeflag: tar.TypeSymlink, Linkname: "elsewhere"}, []string{}},
		{"whiteout subtree", &tar.Header{Name: ".wh.parent", Typeflag: tar.TypeReg}, []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			layer := metadataTestLayer(t, &tar.Header{Name: "parent/deep/file", Typeflag: tar.TypeReg, Mode: 0644}, test.last)
			stream, err := layer.Uncompressed()
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			facts, err := extractLayerTar(stream, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(test.want, facts.ImplicitDirectories) {
				t.Fatalf("implicit facts = %v, want %v", facts.ImplicitDirectories, test.want)
			}
		})
	}
}

func TestImplicitParentNeverTraversesSymlink(t *testing.T) {
	layer := metadataTestLayer(t, &tar.Header{Name: "actual", Typeflag: tar.TypeDir, Mode: 0755},
		&tar.Header{Name: "actual/nested", Typeflag: tar.TypeDir, Mode: 0755},
		&tar.Header{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "actual"},
		&tar.Header{Name: "alias/nested/new", Typeflag: tar.TypeReg, Mode: 0644})
	stream, err := layer.Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := extractLayerTar(stream, t.TempDir()); err == nil {
		t.Fatal("implicit directory facts followed a symlink parent")
	}
}

func TestVisibilityErrorsDoNotFallThrough(t *testing.T) {
	_, err := effectiveDirectoryMetadata([]string{t.TempDir()}, "private", func(string, string) ([]byte, bool, error) {
		return nil, false, os.ErrPermission
	})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("visibility error = %v", err)
	}
}

func TestUnknownOpaqueValueDoesNotAuthorizeMetadataFallback(t *testing.T) {
	for _, value := range []string{"", "Y", "unknown"} {
		if _, err := directoryOpaqueBarrier(t.TempDir(), func(string, string) ([]byte, bool, error) {
			return []byte(value), true, nil
		}); err == nil {
			t.Fatalf("unknown opaque value %q was accepted", value)
		}
	}
}

func TestWhiteoutValidationPrecedesFilesystemChanges(t *testing.T) {
	for _, name := range []string{".wh.", ".wh..", ".wh...", "uncreated/.wh..."} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "layer")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			canary := filepath.Join(parent, "fixture")
			if err := os.WriteFile(canary, []byte("local fixture only"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := applyOCIWhiteout(root, name); err == nil {
				t.Fatal("invalid whiteout was accepted")
			}
			data, err := os.ReadFile(canary)
			if err != nil || string(data) != "local fixture only" {
				t.Fatalf("adjacent fixture changed: %q, %v", data, err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid whiteout changed extraction root: %v, %v", entries, err)
			}
		})
	}
}
