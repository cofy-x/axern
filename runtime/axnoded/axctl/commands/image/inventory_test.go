package image

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildImageRowsMergesImportedAndMounted(t *testing.T) {
	rows := buildImageRows(testInventory())

	if len(rows) != 3 {
		t.Fatalf("buildImageRows() length = %d, want 3: %+v", len(rows), rows)
	}
	if rows[0].ImageRef != "busybox:latest" || rows[1].ImageRef != "python:3.12-slim" || rows[2].ImageRef != "redis:7" {
		t.Fatalf("buildImageRows() order = %+v, want sorted by ref", rows)
	}

	importedOnly := rows[0]
	if !importedOnly.Imported || importedOnly.Mounted {
		t.Fatalf("imported-only row = %+v, want imported yes mounted no", importedOnly)
	}
	mountedOnly := rows[2]
	if mountedOnly.Imported || !mountedOnly.Mounted {
		t.Fatalf("mounted-only row = %+v, want imported no mounted yes", mountedOnly)
	}
	merged := rows[1]
	if !merged.Imported || !merged.Mounted || merged.MountType != "oci" || merged.ArchivePath == "" || merged.ArchiveDigest == "" || merged.CacheKey == "" {
		t.Fatalf("merged row = %+v, want imported and mounted metadata", merged)
	}
}

func TestFindImageRowMatchesContentIdentity(t *testing.T) {
	rows := buildImageRows(testInventory())
	for _, key := range []string{
		"sha256:pythonimport0000000000000000000000000000000000000000000000000000",
		"python:3.12-slim@sha256:pythonimport0000000000000000000000000000000000000000000000000000",
	} {
		row, ok := findImageRow(rows, key)
		if !ok {
			t.Fatalf("findImageRow(%q) ok = false, want true", key)
		}
		if row.ImageRef != "python:3.12-slim" {
			t.Fatalf("findImageRow(%q) = %+v, want python row", key, row)
		}
	}
}

func TestFindImageRowMissing(t *testing.T) {
	_, ok := findImageRow(buildImageRows(testInventory()), "missing:latest")
	if ok {
		t.Fatal("findImageRow() ok = true, want false")
	}
}

func TestRenderImageListTable(t *testing.T) {
	var out bytes.Buffer
	renderImageListTable(&out, buildImageRows(testInventory()))

	got := out.String()
	for _, want := range []string{"REF", "IMPORTED", "MOUNTED", "busybox:latest", "yes", "no", "python:3.12-slim"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderImageListTable() missing %q:\n%s", want, got)
		}
	}
}

func TestRenderImageInspect(t *testing.T) {
	row, ok := findImageRow(buildImageRows(testInventory()), "python:3.12-slim")
	if !ok {
		t.Fatal("test row not found")
	}
	var out bytes.Buffer
	renderImageInspect(&out, row)

	got := out.String()
	for _, want := range []string{
		"Image Ref: python:3.12-slim",
		"Imported: yes",
		"Archive Path: /var/lib/imagemgr/oci/imports/python.tar",
		"Archive Digest: sha256:pythonimport0000000000000000000000000000000000000000000000000000",
		"Mounted: yes",
		"Mount Type: oci",
		"Mount Path: /var/lib/imagemgr/oci/mounts/python/merged",
		"Cache Key: python:3.12-slim@sha256:pythonimport0000000000000000000000000000000000000000000000000000",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderImageInspect() missing %q:\n%s", want, got)
		}
	}
}

func TestRenderMountsJSONShape(t *testing.T) {
	var out bytes.Buffer
	if err := renderJSON(&out, mountsJSON{Mounts: sortedMounts(testInventory())}); err != nil {
		t.Fatalf("renderJSON() error: %v", err)
	}
	var decoded struct {
		Mounts []mountedImageDetail `json:"mounts"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode mounts JSON: %v", err)
	}
	if len(decoded.Mounts) != 2 {
		t.Fatalf("decoded mounts length = %d, want 2", len(decoded.Mounts))
	}
	if decoded.Mounts[0].ImageURL != "python:3.12-slim" || decoded.Mounts[1].ImageURL != "redis:7" {
		t.Fatalf("decoded mounts order = %+v, want sorted", decoded.Mounts)
	}
	if decoded.Mounts[0].CacheKey == "" {
		t.Fatalf("decoded mount missing cache key: %+v", decoded.Mounts[0])
	}
}

func TestRenderImageListJSONShape(t *testing.T) {
	var out bytes.Buffer
	if err := renderJSON(&out, imageListJSON{Images: buildImageRows(testInventory())}); err != nil {
		t.Fatalf("renderJSON() error: %v", err)
	}
	var decoded struct {
		Images []imageRow `json:"images"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode image list JSON: %v", err)
	}
	if len(decoded.Images) != 3 {
		t.Fatalf("decoded images length = %d, want 3", len(decoded.Images))
	}
	if !decoded.Images[1].Imported || !decoded.Images[1].Mounted {
		t.Fatalf("decoded merged image = %+v, want imported and mounted", decoded.Images[1])
	}
	if decoded.Images[1].ArchiveDigest == "" || decoded.Images[1].CacheKey == "" {
		t.Fatalf("decoded merged image missing content identity = %+v", decoded.Images[1])
	}
}

func TestRenderImportResponseJSONShape(t *testing.T) {
	var out bytes.Buffer
	resp := importResponse{
		ImageRef:       "python:3.12-slim",
		ArchivePath:    "/tmp/python.tar",
		ArchiveDigest:  "sha256:pythonimport0000000000000000000000000000000000000000000000000000",
		SizeBytes:      123,
		ImportedAtUnix: 1700000000,
	}
	if err := json.NewEncoder(&out).Encode(resp); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	var decoded importResponse
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode import response JSON: %v", err)
	}
	if decoded.ImageRef != resp.ImageRef || decoded.SizeBytes != resp.SizeBytes || decoded.ArchiveDigest != resp.ArchiveDigest {
		t.Fatalf("decoded import response = %+v, want %+v", decoded, resp)
	}
}

func testInventory() *inventoryResponse {
	return &inventoryResponse{
		ImportedImages: []importedImageDetail{
			{
				ImageRef:       "python:3.12-slim",
				ArchivePath:    "/var/lib/imagemgr/oci/imports/python.tar",
				ArchiveDigest:  "sha256:pythonimport0000000000000000000000000000000000000000000000000000",
				SizeBytes:      123296768,
				ImportedAtUnix: 1700000000,
			},
			{
				ImageRef:       "busybox:latest",
				ArchivePath:    "/var/lib/imagemgr/oci/imports/busybox.tar",
				ArchiveDigest:  "sha256:busyboximport0000000000000000000000000000000000000000000000000000",
				SizeBytes:      1024,
				ImportedAtUnix: 1700000100,
			},
		},
		MountedImages: []mountedImageDetail{
			{
				ImageURL:      "redis:7",
				CacheKey:      "redis:7",
				MountType:     "nydus",
				MountPath:     "/var/lib/imagemgr/nydus/redis",
				NydusImageURL: "redis:7-nydus",
			},
			{
				ImageURL:  "python:3.12-slim",
				CacheKey:  "python:3.12-slim@sha256:pythonimport0000000000000000000000000000000000000000000000000000",
				MountType: "oci",
				MountPath: "/var/lib/imagemgr/oci/mounts/python/merged",
			},
		},
	}
}
