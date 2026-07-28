package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxaccess"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestComputerUseStatusAndScreenshot(t *testing.T) {
	socketPath, shutdown := startComputerUseTestServer(t)
	defer shutdown()
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": &runtimeSpyHandler{name: "runsc"}})
	storeRunningComputerUseContainer(t, s, "axctl-computer-use", socketPath, "health,status,supervisor,file,process,pty,computer_use")

	statusResp, err := s.ComputerUseStatus(context.Background(), &apipb.ComputerUseStatusRequest{ID: "axctl-computer-use"})
	assert.NoError(t, err)
	assert.True(t, statusResp.GetAvailable())
	assert.Equal(t, ":99", statusResp.GetDisplay())
	assert.Equal(t, "x11", statusResp.GetBackend())

	screenResp, err := s.ComputerUseScreenshot(context.Background(), &apipb.ComputerUseScreenshotRequest{ID: "axctl-computer-use"})
	assert.NoError(t, err)
	assert.Equal(t, "image/png", screenResp.GetContentType())
	assert.Equal(t, []byte("\x89PNG\r\n\x1a\n"), screenResp.GetData())

	displayResp, err := s.ComputerUseDisplay(context.Background(), &apipb.ComputerUseDisplayRequest{ID: "axctl-computer-use"})
	assert.NoError(t, err)
	assert.Equal(t, int32(1280), displayResp.GetWidth())
	assert.Equal(t, int32(720), displayResp.GetHeight())

	_, err = s.ComputerUseMouse(context.Background(), &apipb.ComputerUseMouseRequest{ID: "axctl-computer-use", Action: "click", X: 7, Y: 9, Button: "1"})
	assert.NoError(t, err)
	_, err = s.ComputerUseKeyboard(context.Background(), &apipb.ComputerUseKeyboardRequest{ID: "axctl-computer-use", Text: "hello"})
	assert.NoError(t, err)
}

func TestComputerUseRequiresCapabilityLabel(t *testing.T) {
	socketPath, shutdown := startUnavailableProviderTestServer(t, sandboxaccess.CapabilityComputerUse, `screenshot_tool unavailable: import failed`)
	defer shutdown()
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": &runtimeSpyHandler{name: "runsc"}})
	storeRunningComputerUseContainer(t, s, "axctl-computer-use-missing", socketPath, "health,status,supervisor,file,process,pty")

	_, err := s.ComputerUseStatus(context.Background(), &apipb.ComputerUseStatusRequest{ID: "axctl-computer-use-missing"})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "screenshot_tool unavailable")
}

func storeRunningComputerUseContainer(t *testing.T, s *sandboxService, id string, socketPath string, capabilities string) {
	t.Helper()
	s.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{
		ID:             id,
		RuntimeHandler: "runsc",
		Labels: map[string]string{
			runtimesandboxd.LabelReady:        "true",
			runtimesandboxd.LabelSocket:       socketPath,
			runtimesandboxd.LabelCapabilities: capabilities,
		},
	})
	time.Sleep(200 * time.Millisecond)
}

func startComputerUseTestServer(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "axcu-")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/computer-use/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"available":true,"display":":99","backend":"x11"}`)
	})
	mux.HandleFunc("/computer-use/screenshot", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	})
	mux.HandleFunc("/computer-use/display", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"display":":99","backend":"x11","width":1280,"height":720}`)
	})
	mux.HandleFunc("/computer-use/mouse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/computer-use/keyboard", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"protocolVersion":1,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1.5,"socketPath":"`+socketPath+`","userProcess":{"state":"running","pid":11}},"capabilities":["computer_use","file","process"],"providers":[{"name":"computer_use","state":"available","available":true,"capabilities":["computer_use"]}],"computerUse":{"available":true,"display":":99","backend":"x11"}}`)
	})
	server := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return socketPath, func() {
		_ = server.Close()
		_ = os.RemoveAll(dir)
		if err := <-errCh; err != nil {
			t.Fatalf("computer-use test server: %v", err)
		}
	}
}
