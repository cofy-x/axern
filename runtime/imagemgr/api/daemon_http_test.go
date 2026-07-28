package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
)

func TestHttpWorker_CleanupDaemon(t *testing.T) {
	tests := []struct {
		name    string
		req     *CleanupDaemonRequest
		wantErr bool
	}{
		{
			name:    "valid cleanup request",
			req:     &CleanupDaemonRequest{DaemonID: "test-daemon-id"},
			wantErr: false,
		},
		{
			name:    "empty daemon ID",
			req:     &CleanupDaemonRequest{DaemonID: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newMockManager()
			worker := mustNewHttpWorker(t, mgr)

			err := worker.CleanupDaemon(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("CleanupDaemon() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHttpWorker_ListDaemons(t *testing.T) {
	expectedDaemons := []imagefsd.DaemonInfo{
		{
			ID:         "daemon-1",
			Name:       "test-daemon-1",
			MountPoint: "/mnt/daemon-1",
			SourceType: "oss",
			IsAlive:    true,
		},
		{
			ID:         "daemon-2",
			Name:       "test-daemon-2",
			MountPoint: "/mnt/daemon-2",
			SourceType: "nydus",
			IsAlive:    false,
		},
	}

	mgr := &mockManager{
		listDaemonsFunc: func() []imagefsd.DaemonInfo {
			return expectedDaemons
		},
	}

	worker := mustNewHttpWorker(t, mgr)
	daemons, err := worker.ListDaemons()
	if err != nil {
		t.Fatalf("ListDaemons() error = %v", err)
	}

	if len(daemons) != len(expectedDaemons) {
		t.Errorf("ListDaemons() returned %d daemons, want %d", len(daemons), len(expectedDaemons))
	}

	for i, daemon := range daemons {
		if daemon.ID != expectedDaemons[i].ID {
			t.Errorf("Daemon[%d].ID = %s, want %s", i, daemon.ID, expectedDaemons[i].ID)
		}
		if daemon.IsAlive != expectedDaemons[i].IsAlive {
			t.Errorf("Daemon[%d].IsAlive = %v, want %v", i, daemon.IsAlive, expectedDaemons[i].IsAlive)
		}
	}
}

func TestHttpHandler_ListDaemons(t *testing.T) {
	expectedDaemons := []imagefsd.DaemonInfo{
		{ID: "test-1", Name: "daemon-1", IsAlive: true},
	}

	mgr := &mockManager{
		listDaemonsFunc: func() []imagefsd.DaemonInfo {
			return expectedDaemons
		},
	}

	worker := mustNewHttpWorker(t, mgr)
	handler := worker.prepareHttp()

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{
			name:           "valid GET request",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid POST method",
			method:         http.MethodPost,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/list_daemons", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Status code = %d, want %d", w.Code, tt.expectedStatus)
			}

			if w.Code == http.StatusOK {
				var daemons []imagefsd.DaemonInfo
				if err := json.NewDecoder(w.Body).Decode(&daemons); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if len(daemons) != len(expectedDaemons) {
					t.Errorf("Got %d daemons, want %d", len(daemons), len(expectedDaemons))
				}
			}
		})
	}
}

func TestHttpHandler_CleanupDaemon(t *testing.T) {
	mgr := newMockManager()
	worker := mustNewHttpWorker(t, mgr)
	handler := worker.prepareHttp()

	tests := []struct {
		name           string
		method         string
		body           interface{}
		expectedStatus int
	}{
		{
			name:   "valid cleanup request",
			method: http.MethodPost,
			body: CleanupDaemonRequest{
				DaemonID: "test-daemon-id",
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid method GET",
			method:         http.MethodGet,
			body:           nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "empty daemon ID",
			method: http.MethodPost,
			body: CleanupDaemonRequest{
				DaemonID: "",
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body *bytes.Reader
			if tt.body != nil {
				jsonData, _ := json.Marshal(tt.body)
				body = bytes.NewReader(jsonData)
			} else {
				body = bytes.NewReader(nil)
			}

			req := httptest.NewRequest(tt.method, "/cleanup_daemon", body)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Status code = %d, want %d. Body: %s", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

func TestHttpWorker_Inventory(t *testing.T) {
	expectedDaemons := []imagefsd.DaemonInfo{
		{
			ID:         "test-1",
			Name:       "daemon-1",
			MountPoint: "/mnt/daemon-1",
			SourceType: "nydus",
			ImageURL:   "reg.example/alpine:nydus",
			IsAlive:    true,
		},
	}
	mgr := &mockManager{
		listDaemonsFunc: func() []imagefsd.DaemonInfo {
			return expectedDaemons
		},
		chunkDBStatsFunc: func() (*imagefsd.ChunkDBStats, error) {
			stats := &imagefsd.ChunkDBStats{}
			stats.Chunks.TotalCount = 12
			stats.Storage.UsedSizeBytes = 128
			stats.Storage.UsagePercent = "25.5"
			return stats, nil
		},
		localityStatsFunc: func() (*imagefsd.LocalityStats, error) {
			return &imagefsd.LocalityStats{
				ChunkDBTotalChunks:        12,
				ChunkDBUsedBytes:          128,
				ChunkDBRecentAccessAgeSec: 9,
				PeerHealthyCount:          3,
				PeerUnhealthyCount:        1,
				PeerHintedCount:           2,
			}, nil
		},
	}
	worker := mustNewHttpWorker(t, mgr)
	worker.mountStore = nil
	worker.ociMgr = nil
	workerInventory, err := worker.Inventory()
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	if len(workerInventory.Daemons) != 1 {
		t.Fatalf("Inventory() daemons = %d, want 1", len(workerInventory.Daemons))
	}
	if len(workerInventory.Locality) != 1 {
		t.Fatalf("Inventory() locality = %d, want 1", len(workerInventory.Locality))
	}
	if workerInventory.Locality[0].Key != "image:reg.example/alpine:nydus" {
		t.Fatalf("Inventory() locality key = %s", workerInventory.Locality[0].Key)
	}
	if workerInventory.Locality[0].PeerHealthyCount != 3 {
		t.Fatalf("Inventory() peer_healthy_count = %d", workerInventory.Locality[0].PeerHealthyCount)
	}
	if workerInventory.ChunkDB == nil || workerInventory.ChunkDB.Chunks.TotalCount != 12 {
		t.Fatalf("Inventory() chunkdb = %+v", workerInventory.ChunkDB)
	}
}

func TestHttpWorker_InventoryLocalityDegradesWhenStatsUnavailable(t *testing.T) {
	mgr := &mockManager{
		listDaemonsFunc: func() []imagefsd.DaemonInfo {
			return []imagefsd.DaemonInfo{{
				ID:           "daemon-1",
				Name:         "rootfs.raw",
				MountPoint:   "/mnt/daemon-1",
				SourceType:   "oss",
				Endpoint:     "minio:9000",
				Bucket:       "dist",
				ObjectPrefix: "images/",
				IsAlive:      false,
			}}
		},
		localityStatsFunc: func() (*imagefsd.LocalityStats, error) {
			return nil, fmt.Errorf("chunk socket unavailable")
		},
	}
	worker := mustNewHttpWorker(t, mgr)
	worker.mountStore = nil
	worker.ociMgr = nil

	resp, err := worker.Inventory()
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	if len(resp.Locality) != 1 {
		t.Fatalf("Inventory() locality = %d, want 1", len(resp.Locality))
	}
	if resp.LocalityError == "" {
		t.Fatalf("Inventory() locality_error is empty")
	}
	if resp.Locality[0].Key != "s3:minio:9000/dist/images/rootfs.raw" {
		t.Fatalf("Inventory() locality key = %s", resp.Locality[0].Key)
	}
	if resp.Locality[0].ChunkDBTotalChunks != 0 {
		t.Fatalf("Inventory() locality chunk count = %d, want 0", resp.Locality[0].ChunkDBTotalChunks)
	}
}

func TestHttpHandler_Inventory(t *testing.T) {
	mgr := &mockManager{
		listDaemonsFunc: func() []imagefsd.DaemonInfo {
			return []imagefsd.DaemonInfo{{ID: "daemon-1", SourceType: "nydus", IsAlive: true}}
		},
		chunkDBStatsFunc: func() (*imagefsd.ChunkDBStats, error) {
			stats := &imagefsd.ChunkDBStats{}
			stats.Chunks.TotalCount = 4
			stats.Storage.UsedSizeBytes = 64
			stats.Storage.UsagePercent = "10"
			return stats, nil
		},
	}
	worker := &HttpWorker{mgr: mgr, mountStore: nil}
	handler := worker.prepareHttp()

	req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp InventoryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode inventory response: %v", err)
	}
	if len(resp.Daemons) != 1 {
		t.Fatalf("got %d daemons, want 1", len(resp.Daemons))
	}
	if resp.ChunkDB == nil || resp.ChunkDB.Chunks.TotalCount != 4 {
		t.Fatalf("unexpected chunkdb response: %+v", resp.ChunkDB)
	}
}
