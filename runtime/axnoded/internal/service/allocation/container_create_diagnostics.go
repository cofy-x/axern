package allocation

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/trace"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/sirupsen/logrus"
)

const createFailureSnippetBytes = 4096

type sandboxdFailureContext struct {
	ContainerID      string                    `json:"containerId"`
	SocketPath       string                    `json:"socketPath"`
	SocketError      string                    `json:"socketError,omitempty"`
	Ready            string                    `json:"ready,omitempty"`
	Capabilities     string                    `json:"capabilities,omitempty"`
	UserState        string                    `json:"userState,omitempty"`
	ProviderSummary  *wire.ProviderSummary     `json:"providerSummary,omitempty"`
	ProcessSummary   *wire.ProcessSummary      `json:"processSummary,omitempty"`
	DiagnosticsError string                    `json:"diagnosticsError,omitempty"`
	Diagnostics      *wire.DiagnosticsResponse `json:"diagnostics,omitempty"`
}

func (h *Controller) logSandboxdDiagnostics(traceID, containerID string, metaData *apipb.ContainerMetadata) {
	report := sandboxdFailureContext{
		ContainerID: containerID,
		SocketPath:  h.sandboxdSocketPath(containerID, metaData),
	}
	if metaData != nil {
		labels := metaData.GetLabels()
		report.Ready = labels[runtimesandboxd.LabelReady]
		report.Capabilities = labels[runtimesandboxd.LabelCapabilities]
		report.UserState = labels[runtimesandboxd.LabelUserState]
	}
	if _, err := os.Stat(report.SocketPath); err != nil {
		report.SocketError = err.Error()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		diagnostics, err := runtimesandboxd.NewClient(report.SocketPath).Diagnostics(ctx)
		if err != nil {
			report.DiagnosticsError = err.Error()
		} else {
			report.ProviderSummary = &diagnostics.ProviderSummary
			report.ProcessSummary = &diagnostics.ProcessSummary
			report.Diagnostics = &diagnostics
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("marshal failed container %s sandboxd diagnostics failed: %v", containerID, err)
		return
	}
	logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("failed container %s sandboxd failure context: %s", containerID, string(data))
}

func (h *Controller) sandboxdSocketPath(containerID string, metaData *apipb.ContainerMetadata) string {
	if metaData != nil {
		if snapshot, err := runtimesandboxd.SnapshotFromLabels(metaData.GetLabels()); err == nil && snapshot.SocketPath != "" {
			return snapshot.SocketPath
		}
	}
	return runtimeoci.SandboxdBundleSocketPath(filepath.Join(h.config.RootDir, "containers", containerID))
}

func (h *Controller) logStdFileSnippet(traceID, containerID, name, path string) {
	if path == "" {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("open failed container %s %s failed: %v", containerID, name, err)
		}
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("stat failed container %s %s failed: %v", containerID, name, err)
		return
	}
	offset := max(info.Size()-createFailureSnippetBytes, 0)
	if _, err := file.Seek(offset, 0); err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("seek failed container %s %s failed: %v", containerID, name, err)
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("read failed container %s %s failed: %v", containerID, name, err)
		return
	}
	snippet := strings.TrimSpace(string(data))
	if snippet == "" {
		return
	}
	logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("failed container %s %s tail: %s", containerID, name, snippet)
}

func (h *Controller) cleanupStdFile(traceID, path string) {
	if path == "" {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("clean std file failed: %v", err)
	}
}
