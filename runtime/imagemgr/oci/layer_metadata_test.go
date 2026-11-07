package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sys/unix"
)

func TestExtractLayerGuestDirectoryMetadataUnderPrivateUmask(t *testing.T) {
	oldUmask := syscall.Umask(0077)
	defer syscall.Umask(oldUmask)
	for _, explicitRoot := range []bool{false, true} {
		name := "default root"
		if explicitRoot {
			name = "explicit root"
		}
		t.Run(name, func(t *testing.T) {
			dst := t.TempDir()
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			writeHeader := func(header *tar.Header) {
				t.Helper()
				header.Uid, header.Gid = os.Getuid(), os.Getgid()
				if err := tw.WriteHeader(header); err != nil {
					t.Fatal(err)
				}
			}
			if explicitRoot {
				writeHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 01750})
			}
			writeHeader(&tar.Header{Name: "private", Typeflag: tar.TypeDir, Mode: 0700})
			for _, path := range []string{"implicit/deep/input", "private/input", "whiteout/deep/.wh.removed"} {
				writeHeader(&tar.Header{Name: path, Typeflag: tar.TypeReg, Mode: 0644})
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := extractLayerTar(bytes.NewReader(buf.Bytes()), dst); err != nil {
				t.Fatal(err)
			}
			rootMode := os.FileMode(0755)
			if explicitRoot {
				rootMode = 0750 | os.ModeSticky
			}
			assertDirectoryMode(t, dst, rootMode)
			for _, path := range []string{"implicit", "implicit/deep", "whiteout", "whiteout/deep"} {
				assertDirectoryMode(t, filepath.Join(dst, path), 0755)
			}
			assertDirectoryMode(t, filepath.Join(dst, "private"), 0700)
			clone := filepath.Join(t.TempDir(), "clone")
			if err := buildHardlinkTree(dst, clone); err != nil {
				t.Fatal(err)
			}
			assertDirectoryMode(t, clone, rootMode)
			assertDirectoryMode(t, filepath.Join(clone, "implicit/deep"), 0755)
			assertDirectoryMode(t, filepath.Dir(clone), 0700)
		})
	}
}

func TestExtractedLayerWarmAndCrashRecoveryRetainGuestModes(t *testing.T) {
	oldUmask := syscall.Umask(0077)
	defer syscall.Umask(oldUmask)
	mgr := newTestManager(t)
	layer := sleepLayer{digest: v1.Hash{Algorithm: "sha256", Hex: "metadata"}}
	record, err := mgr.ensureLayerExtracted(layer)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := mgr.ensureLayerExtracted(panicUncompressedLayer{digest: layer.digest})
		if err != nil {
			t.Fatal(err)
		}
		assertDirectoryMode(t, got.Path, 0755)
		assertDirectoryMode(t, filepath.Join(got.Path, "etc"), 0755)
		assertDirectoryMode(t, filepath.Dir(got.Path), 0700)
	}
	_ = record

}

func TestLayerCrashRecoveryDoesNotGuessMissingRootHeaderFact(t *testing.T) {
	mgr := newTestManager(t)
	layer := panicUncompressedLayer{digest: v1.Hash{Algorithm: "sha256", Hex: "unavailable-root-metadata"}}
	dir, err := mgr.store.getOrCreateLayerDir(layer.digest.String())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(mgr.layersDir, dir, "fs")
	if err := os.MkdirAll(root, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0750); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ensureLayerExtracted(layer); err == nil {
		t.Fatal("crash recovery accepted an unknown root-header fact")
	}
	record, err := mgr.store.getLayer(layer.digest.String())
	if err != nil || record != nil {
		t.Fatalf("failed recovery published layer metadata: %+v, %v", record, err)
	}
	assertDirectoryMode(t, root, 0750)
}

func TestLayerHardlinkSourceRemainsWithinExtractionRoot(t *testing.T) {
	outside := t.TempDir()
	canary := filepath.Join(outside, "canary")
	if err := os.WriteFile(canary, []byte("local fixture only"), 0644); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	for _, header := range []*tar.Header{
		{Name: "parent", Typeflag: tar.TypeSymlink, Linkname: outside},
		{Name: "linked", Typeflag: tar.TypeLink, Linkname: "parent/canary"},
	} {
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := extractLayerTar(bytes.NewReader(data.Bytes()), root); err == nil {
		t.Fatal("hardlink source escaped its layer root")
	}
	if _, err := os.Lstat(filepath.Join(root, "linked")); !os.IsNotExist(err) {
		t.Fatalf("outside source was linked into the layer: %v", err)
	}
	got, err := os.ReadFile(canary)
	if err != nil || string(got) != "local fixture only" {
		t.Fatalf("outside fixture changed: %q, %v", got, err)
	}
}

func TestChainRootMetadataInheritsAndExplicitlyOverrides(t *testing.T) {
	oldUmask := syscall.Umask(0077)
	defer syscall.Umask(oldUmask)
	mgr := newTestManager(t)
	makeLayer := func(name string, mode int64, uid, gid int) v1.Layer {
		t.Helper()
		var data bytes.Buffer
		writer := tar.NewWriter(&data)
		if mode != 0 {
			if err := writer.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir,
				Mode: mode, Uid: uid, Gid: gid}); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644}); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return static.NewLayer(data.Bytes(), types.OCIUncompressedLayer)
	}
	baseUID, baseGID := os.Getuid(), os.Getgid()
	layers := []v1.Layer{makeLayer("base", 0750, baseUID, baseGID),
		makeLayer("inherited", 0, 0, 0), makeLayer("override", 0711, os.Getuid(), os.Getgid())}
	layerFacts, err := mgr.extractLayersWithWorkers(context.Background(), layers)
	if err != nil {
		t.Fatal(err)
	}
	if !layerFacts[0].HasRootMetadata || layerFacts[1].HasRootMetadata || !layerFacts[2].HasRootMetadata {
		t.Fatalf("root header facts = %v", layerFacts)
	}
	_, paths := testLayerRecordFields(layerFacts)
	// Root whiteout semantics belong to the source layer, not inherited stat metadata.
	xattrs := unix.Setxattr(paths[0], "user.overlay.opaque", []byte("y"), 0) == nil
	diffIDs := make([]v1.Hash, len(layers))
	for i, layer := range layers {
		diffIDs[i], err = layer.DiffID()
		if err != nil {
			t.Fatal(err)
		}
	}
	chainIDs, err := buildChainIDs(diffIDs)
	if err != nil {
		t.Fatal(err)
	}
	chains, err := mgr.ensureChainLowerDirs(chainIDs, layerFacts)
	if err != nil {
		t.Fatal(err)
	}
	for i, mode := range []os.FileMode{0750, 0750, 0711} {
		assertDirectoryMode(t, chains[i], mode)
		info, err := os.Stat(chains[i])
		if err != nil {
			t.Fatal(err)
		}
		wantUID, wantGID := baseUID, baseGID
		if i == 2 {
			wantUID, wantGID = os.Getuid(), os.Getgid()
		}
		stat := info.Sys().(*syscall.Stat_t)
		if int(stat.Uid) != wantUID || int(stat.Gid) != wantGID {
			t.Fatalf("chain %d owner = %d:%d, want %d:%d", i, stat.Uid, stat.Gid, wantUID, wantGID)
		}
	}
	if xattrs {
		if _, err := unix.Getxattr(chains[1], "user.overlay.opaque", nil); err != unix.ENODATA {
			t.Fatalf("child inherited parent root opaque xattr: %v", err)
		}
	}
	assertDirectoryMode(t, paths[1], 0755)
	if err := mgr.store.deleteChain(chainIDs[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ensureChainLowerDirs(chainIDs, layerFacts); err != nil {
		t.Fatal(err)
	}
	assertDirectoryMode(t, chains[1], 0750)
}

func TestChainRootInheritsNonRootOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("creating foreign-owned root metadata requires root")
	}
	mgr := newTestManager(t)
	base, child := t.TempDir(), t.TempDir()
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(base, 12345, 12346); err != nil {
		t.Fatal(err)
	}
	chains, err := mgr.ensureChainLowerDirs([]string{"base-owner", "child-owner"},
		[]*LayerRecord{{Path: base, HasRootMetadata: true, ImplicitDirectories: []string{}}, {Path: child, ImplicitDirectories: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range chains {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Uid != 12345 || stat.Gid != 12346 {
			t.Fatalf("chain root owner = %d:%d, want 12345:12346", stat.Uid, stat.Gid)
		}
	}
}

func TestLayerRootLastHeaderAndExplicitParentMetadataWin(t *testing.T) {
	oldUmask := syscall.Umask(0077)
	defer syscall.Umask(oldUmask)
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	for _, header := range []*tar.Header{
		{Name: "./", Typeflag: tar.TypeDir, Mode: 0750},
		{Name: "parent/before", Typeflag: tar.TypeReg, Mode: 0644},
		{Name: "parent", Typeflag: tar.TypeDir, Mode: 0710},
		{Name: "parent/after", Typeflag: tar.TypeReg, Mode: 0644},
		{Name: "/", Typeflag: tar.TypeDir, Mode: 01777},
	} {
		header.Uid, header.Gid = os.Getuid(), os.Getgid()
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := extractLayerTar(bytes.NewReader(data.Bytes()), root); err != nil {
		t.Fatal(err)
	}
	assertDirectoryMode(t, root, 0777|os.ModeSticky)
	assertDirectoryMode(t, filepath.Join(root, "parent"), 0710)
}

func assertDirectoryMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode()&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != want {
		t.Fatalf("directory %s mode = %v, want %v", path, info.Mode(), want)
	}
}
