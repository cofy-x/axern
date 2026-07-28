package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHttpWorker_MountOSS_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *OSSMountRequest
		wantErr bool
	}{
		{
			name: "invalid object - trailing slash",
			req: &OSSMountRequest{
				Endpoint: "oss-cn-hangzhou.aliyuncs.com",
				Bucket:   "test-bucket",
				Object:   "images/",
			},
			wantErr: true,
		},
		{
			name: "invalid object - empty",
			req: &OSSMountRequest{
				Endpoint: "oss-cn-hangzhou.aliyuncs.com",
				Bucket:   "test-bucket",
				Object:   "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newMockManager()
			worker := mustNewHttpWorker(t, mgr)

			_, err := worker.MountOSS(t.Context(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("MountOSS() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHttpHandler_OSSMount(t *testing.T) {
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
			name:           "invalid method GET",
			method:         http.MethodGet,
			body:           nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			method:         http.MethodPost,
			body:           "invalid json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "invalid object",
			method: http.MethodPost,
			body: OSSMountRequest{
				Endpoint: "oss-cn-hangzhou.aliyuncs.com",
				Bucket:   "test-bucket",
				Object:   "images/",
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					body = bytes.NewBufferString(str)
				} else {
					jsonData, _ := json.Marshal(tt.body)
					body = bytes.NewReader(jsonData)
				}
			}

			req := httptest.NewRequest(tt.method, "/oss_mount", body)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Status code = %d, want %d. Body: %s", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

func TestHttpHandler_OSSUmount(t *testing.T) {
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
			name:           "invalid method GET",
			method:         http.MethodGet,
			body:           nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "daemon not found",
			method: http.MethodPost,
			body: OSSUmountRequest{
				Endpoint: "oss-cn-hangzhou.aliyuncs.com",
				Bucket:   "test-bucket",
				Object:   "test.tar",
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != nil {
				jsonData, _ := json.Marshal(tt.body)
				body = bytes.NewReader(jsonData)
			}

			req := httptest.NewRequest(tt.method, "/oss_umount", body)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Status code = %d, want %d", w.Code, tt.expectedStatus)
			}
		})
	}
}
