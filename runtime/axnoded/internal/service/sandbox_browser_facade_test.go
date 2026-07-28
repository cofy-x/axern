package service

import (
	"context"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxaccess"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBrowserOperations(t *testing.T) {
	socketPath, shutdown := startBrowserTestServer(t)
	defer shutdown()
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": &runtimeSpyHandler{name: "runsc"}})
	storeRunningBrowserContainer(t, s, "axctl-browser", socketPath, "health,status,supervisor,file,process,pty,browser")

	statusResp, err := s.BrowserStatus(context.Background(), &apipb.BrowserStatusRequest{ID: "axctl-browser"})
	require.NoError(t, err)
	assert.True(t, statusResp.GetAvailable())
	assert.Equal(t, "chromium", statusResp.GetCommand())

	openResp, err := s.BrowserOpen(context.Background(), &apipb.BrowserOpenRequest{ID: "axctl-browser", Url: "data:text/html,open"})
	require.NoError(t, err)
	assert.True(t, openResp.GetRunning())
	assert.Equal(t, int32(88), openResp.GetPid())
	assert.Equal(t, "data:text/html,open", openResp.GetUrl())

	navigateResp, err := s.BrowserNavigate(context.Background(), &apipb.BrowserNavigateRequest{ID: "axctl-browser", Url: "data:text/html,navigate"})
	require.NoError(t, err)
	assert.Equal(t, "data:text/html,navigate", navigateResp.GetUrl())

	_, err = s.BrowserResize(context.Background(), &apipb.BrowserResizeRequest{ID: "axctl-browser", Width: 1024, Height: 768})
	require.NoError(t, err)
	_, err = s.BrowserClick(context.Background(), &apipb.BrowserClickRequest{ID: "axctl-browser", X: 7, Y: 9, Button: "left"})
	require.NoError(t, err)
	_, err = s.BrowserType(context.Background(), &apipb.BrowserTypeRequest{ID: "axctl-browser", Text: "hello", DelayMs: 5})
	require.NoError(t, err)
	_, err = s.BrowserWait(context.Background(), &apipb.BrowserWaitRequest{ID: "axctl-browser", TimeoutMs: 250})
	require.NoError(t, err)
	closeResp, err := s.BrowserClose(context.Background(), &apipb.BrowserCloseRequest{ID: "axctl-browser"})
	require.NoError(t, err)
	assert.False(t, closeResp.GetRunning())
}

func TestBrowserRequiresCapabilityLabel(t *testing.T) {
	socketPath, shutdown := startUnavailableProviderTestServer(t, sandboxaccess.CapabilityBrowser, `browser_command unavailable: no supported browser command found`)
	defer shutdown()
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": &runtimeSpyHandler{name: "runsc"}})
	storeRunningBrowserContainer(t, s, "axctl-browser-missing", socketPath, "health,status,supervisor,file,process,pty")

	_, err := s.BrowserStatus(context.Background(), &apipb.BrowserStatusRequest{ID: "axctl-browser-missing"})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "browser_command unavailable")
}

func TestSandboxdDiagnosticsUsesContainerProviderSnapshot(t *testing.T) {
	socketPath, shutdown := startBrowserTestServer(t)
	defer shutdown()
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": &runtimeSpyHandler{name: "runsc"}})
	storeRunningBrowserContainer(t, s, "axctl-diagnostics", socketPath, "health,status,supervisor,file,process,pty,browser")

	diagnostics, err := s.SandboxdDiagnostics(context.Background(), "axctl-diagnostics", true)
	require.NoError(t, err)
	assert.Contains(t, diagnostics.Capabilities, "browser")
	browserProviderFound := false
	for _, provider := range diagnostics.Providers {
		if provider.Name == "browser" {
			browserProviderFound = true
			assert.True(t, provider.Available)
		}
	}
	assert.True(t, browserProviderFound)
	assert.NotEmpty(t, diagnostics.RawJSON)
}

func TestBrowserOperationErrorIncludesDiagnosticsDetail(t *testing.T) {
	socketPath, shutdown := startProviderOperationErrorTestServer(t, sandboxaccess.CapabilityBrowser, "/browser/status", `browser crashed`)
	defer shutdown()
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": &runtimeSpyHandler{name: "runsc"}})
	storeRunningBrowserContainer(t, s, "axctl-browser-error", socketPath, "health,status,supervisor,file,process,pty,browser")

	_, err := s.BrowserStatus(context.Background(), &apipb.BrowserStatusRequest{ID: "axctl-browser-error"})

	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "browser crashed")
	assert.Contains(t, err.Error(), "providers 1/1 available")
	assert.Contains(t, err.Error(), "browser provider degraded")
}
