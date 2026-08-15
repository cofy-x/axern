package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	imgobs "github.com/cofy-x/axern/runtime/imagemgr/internal/observability"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

func requireMethod(writer http.ResponseWriter, request *http.Request, method string, errMsg string) bool {
	if request.Method == method {
		return true
	}
	writeText(writer, http.StatusBadRequest, errMsg)
	return false
}

func decodeJSONBody(writer http.ResponseWriter, request *http.Request, dst any, errMsg string) bool {
	if err := json.NewDecoder(request.Body).Decode(dst); err != nil {
		writeText(writer, http.StatusBadRequest, errMsg)
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	data, _ := json.Marshal(body)
	writer.WriteHeader(status)
	writer.Write(data)
}

func writeText(writer http.ResponseWriter, status int, body string) {
	writer.WriteHeader(status)
	writer.Write([]byte(body))
}

func logAPICall(name string, start time.Time, err error) {
	fields := logrus.Fields{"operation": name, "duration_seconds": time.Since(start).Seconds()}
	if err != nil {
		logrus.WithError(err).WithFields(fields).Warn("imagemgr api call failed")
		return
	}
	logrus.WithFields(fields).Info("imagemgr api call completed")
}

func startAPIOperation(request *http.Request, spanName, operation string, attrs ...attribute.KeyValue) (*sdkobs.Operation, *http.Request) {
	spanAttrs := append([]attribute.KeyValue{attribute.String(sdkobs.AttrOperation, operation)}, attrs...)
	ctx, op := sdkobs.StartOperation(request.Context(), sdkobs.OperationConfig{
		Name:        spanName,
		SpanAttrs:   spanAttrs,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, operation)},
		Counter:     imgobs.MetricHTTPOperationTotal,
		Duration:    imgobs.MetricHTTPOperationDuration,
	})
	return op, request.WithContext(ctx)
}

func (w *HttpWorker) prepareHttp() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/oss_mount", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "mount only support post method") {
			return
		}
		var req OSSMountRequest
		if !decodeJSONBody(writer, request, &req, "invalid oss mount request format") {
			return
		}
		start := time.Now()
		info, err := w.MountOSS(request.Context(), &req)
		logAPICall("mount", start, err)
		if err != nil {
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to mount, err = %s", err))
			return
		}
		writeJSON(writer, http.StatusOK, info)
	})

	mux.HandleFunc("/oss_umount", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "unmount only support post method") {
			return
		}
		var req OSSUmountRequest
		if !decodeJSONBody(writer, request, &req, "invalid oss unmount request format") {
			return
		}
		start := time.Now()
		info, err := w.UnmountOSS(request.Context(), &req)
		logAPICall("umount", start, err)
		if err != nil {
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to unmount, err = %s", err))
			return
		}
		writeJSON(writer, http.StatusOK, info)
	})

	mux.HandleFunc("/nydus_mount", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "nydus_mount only supports post method") {
			return
		}
		var req NydusMountRequest
		if !decodeJSONBody(writer, request, &req, "invalid nydus mount request format") {
			return
		}
		op, request := startAPIOperation(request, imgobs.SpanNydusMount, "nydus_mount", attribute.String(sdkobs.AttrImageRef, req.ImageURL))
		var opErr error
		defer func() { op.End(opErr) }()
		start := time.Now()
		info, err := w.MountNydus(request.Context(), &req)
		logAPICall("nydus_mount", start, err)
		if err != nil {
			opErr = err
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to mount nydus image, err = %s", err))
			return
		}
		op.SetHTTPStatusCode(http.StatusOK)
		writeJSON(writer, http.StatusOK, info)
	})

	mux.HandleFunc("/nydus_umount", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "nydus_umount only supports post method") {
			return
		}
		var req NydusUmountRequest
		if !decodeJSONBody(writer, request, &req, "invalid nydus unmount request format") {
			return
		}
		op, request := startAPIOperation(request, imgobs.SpanNydusUnmount, "nydus_unmount", attribute.String(sdkobs.AttrImageRef, req.ImageURL))
		var opErr error
		defer func() { op.End(opErr) }()
		start := time.Now()
		info, err := w.UnmountNydus(request.Context(), &req)
		logAPICall("nydus_umount", start, err)
		if err != nil {
			opErr = err
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to unmount nydus image, err = %s", err))
			return
		}
		op.SetHTTPStatusCode(http.StatusOK)
		writeJSON(writer, http.StatusOK, info)
	})

	mux.HandleFunc("/oci_mount", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "oci_mount only supports post method") {
			return
		}
		var req OCIMountRequest
		if !decodeJSONBody(writer, request, &req, "invalid oci mount request format") {
			return
		}
		op, request := startAPIOperation(request, imgobs.SpanOCIMount, "oci_mount", attribute.String(sdkobs.AttrImageRef, req.ImageURL))
		var opErr error
		defer func() { op.End(opErr) }()
		start := time.Now()
		resp, err := w.MountOCI(request.Context(), &req)
		logAPICall("oci_mount", start, err)
		if err != nil {
			opErr = err
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to mount oci image, err = %s", err))
			return
		}
		op.SetHTTPStatusCode(http.StatusOK)
		writeJSON(writer, http.StatusOK, resp)
	})

	mux.HandleFunc("/oci_umount", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "oci_umount only supports post method") {
			return
		}
		var req OCIUmountRequest
		if !decodeJSONBody(writer, request, &req, "invalid oci umount request format") {
			return
		}
		op, request := startAPIOperation(request, imgobs.SpanOCIUnmount, "oci_unmount", attribute.String(sdkobs.AttrImageRef, req.ImageURL))
		var opErr error
		defer func() { op.End(opErr) }()
		start := time.Now()
		err := w.UnmountOCI(request.Context(), &req)
		logAPICall("oci_umount", start, err)
		if err != nil {
			opErr = err
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to umount oci image, err = %s", err))
			return
		}
		op.SetHTTPStatusCode(http.StatusOK)
		writeText(writer, http.StatusOK, "ok")
	})

	mux.HandleFunc("/reconcile_mount_leases", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "reconcile_mount_leases only supports post method") {
			return
		}
		var req ReconcileMountLeasesRequest
		if !decodeJSONBody(writer, request, &req, "invalid mount lease reconcile request") {
			return
		}
		resp, err := w.ReconcileMountLeases(request.Context(), &req)
		if err != nil {
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to reconcile mount leases, err = %s", err))
			return
		}
		writeJSON(writer, http.StatusOK, resp)
	})

	mux.HandleFunc("/oci_import", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "oci_import only supports post method") {
			return
		}
		imageRef := strings.TrimSpace(request.URL.Query().Get("ref"))
		if imageRef == "" {
			writeText(writer, http.StatusBadRequest, "ref query parameter is required")
			return
		}
		op, request := startAPIOperation(request, imgobs.SpanOCIImport, "oci_import", attribute.String(sdkobs.AttrImageRef, imageRef))
		var opErr error
		defer func() { op.End(opErr) }()
		start := time.Now()
		resp, err := w.ImportOCI(request.Context(), imageRef, request.Body)
		logAPICall("oci_import", start, err)
		if err != nil {
			opErr = err
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to import oci image, err = %s", err))
			return
		}
		op.SetHTTPStatusCode(http.StatusOK)
		writeJSON(writer, http.StatusOK, resp)
	})

	mux.HandleFunc("/oci_resolve", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodGet, "oci_resolve only supports get method") {
			return
		}
		imageRef := strings.TrimSpace(request.URL.Query().Get("ref"))
		if imageRef == "" {
			writeText(writer, http.StatusBadRequest, "ref query parameter is required")
			return
		}
		resp, err := w.ResolveOCI(imageRef)
		if err != nil {
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to resolve oci image, err = %s", err))
			return
		}
		writeJSON(writer, http.StatusOK, resp)
	})

	mux.HandleFunc("/cleanup_daemon", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodPost, "cleanup_daemon only supports post method") {
			return
		}
		var req CleanupDaemonRequest
		if !decodeJSONBody(writer, request, &req, "invalid cleanup daemon request format") {
			return
		}
		start := time.Now()
		err := w.CleanupDaemon(&req)
		logAPICall("cleanup_daemon", start, err)
		if err != nil {
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to cleanup daemon, err = %s", err))
			return
		}
		writeText(writer, http.StatusOK, "ok")
	})

	mux.HandleFunc("/list_daemons", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodGet, "list_daemons only supports GET method") {
			return
		}
		start := time.Now()
		daemons, err := w.ListDaemons()
		logAPICall("list_daemons", start, err)
		if err != nil {
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to list daemons, err = %s", err))
			return
		}
		writeJSON(writer, http.StatusOK, daemons)
	})

	mux.HandleFunc("/list_oci_mounts", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodGet, "list_oci_mounts only supports GET method") {
			return
		}
		start := time.Now()
		imageURLs, err := w.ListMountedOCIImages()
		logAPICall("list_oci_mounts", start, err)
		if err != nil {
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to list oci mounts, err = %s", err))
			return
		}
		writeJSON(writer, http.StatusOK, map[string][]string{"image_urls": imageURLs})
	})

	mux.HandleFunc("/list_oci_mount_details", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodGet, "list_oci_mount_details only supports GET method") {
			return
		}
		start := time.Now()
		mounts, err := w.ListMountedOCIDetails()
		logAPICall("list_oci_mount_details", start, err)
		if err != nil {
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to list oci mount details, err = %s", err))
			return
		}
		writeJSON(writer, http.StatusOK, map[string][]MountedImageDetail{"mounts": mounts})
	})

	mux.HandleFunc("/inventory", func(writer http.ResponseWriter, request *http.Request) {
		if !requireMethod(writer, request, http.MethodGet, "inventory only supports GET method") {
			return
		}
		op, request := startAPIOperation(request, imgobs.SpanInventory, "inventory")
		var opErr error
		defer func() { op.End(opErr) }()
		start := time.Now()
		inventory, err := w.Inventory()
		logAPICall("inventory", start, err)
		if err != nil {
			opErr = err
			writeText(writer, http.StatusInternalServerError, fmt.Sprintf("failed to build inventory, err = %s", err))
			return
		}
		op.SetHTTPStatusCode(http.StatusOK)
		writeJSON(writer, http.StatusOK, inventory)
	})

	return mux
}

func (w *HttpWorker) ServeHTTP(ctx context.Context, sockPath string) error {
	if sockPath == "" {
		return fmt.Errorf("empty socket path")
	}
	mux := w.prepareHttp()
	info, err := os.Lstat(sockPath)
	if err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket path %s", sockPath)
		}
		if err := os.Remove(sockPath); err != nil {
			return fmt.Errorf("remove stale unix socket %s: %w", sockPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect unix socket %s: %w", sockPath, err)
	}
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("failed to create unix socket %s: %w", sockPath, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(sockPath)
	}()
	server := http.Server{
		Handler: sdkobs.HTTPHandler(mux, imgobs.SpanHTTP),
	}
	serveStopped := make(chan struct{})
	shutdownResult := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			shutdownErr := server.Shutdown(shutdownCtx)
			if shutdownErr != nil {
				logrus.WithError(shutdownErr).Warn("imagemgr HTTP server graceful shutdown failed")
				if closeErr := server.Close(); closeErr != nil {
					logrus.WithError(closeErr).Warn("imagemgr HTTP server forced shutdown failed")
				}
			}
			shutdownResult <- shutdownErr
		case <-serveStopped:
			shutdownResult <- nil
		}
	}()

	err = server.Serve(listener)
	close(serveStopped)
	shutdownErr := <-shutdownResult
	if shutdownErr != nil {
		return fmt.Errorf("shutdown imagemgr HTTP server: %w", shutdownErr)
	}
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return err
}
