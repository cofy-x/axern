package sandboxd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func TestClientHealthReadyStatusCapabilities(t *testing.T) {
	socketPath, shutdown := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			fmt.Fprint(w, `{"protocolVersion":1,"status":"ok"}`)
		case "/readyz":
			fmt.Fprint(w, `{"protocolVersion":1,"ready":true}`)
		case "/status":
			fmt.Fprint(w, `{"daemonPid":7,"uptimeSeconds":1.5,"socketPath":"/mnt/axern-sandboxd.sock","userProcess":{"state":"running","pid":11}}`)
		case "/capabilities":
			fmt.Fprint(w, `{"protocolVersion":1,"capabilities":["archive","diagnostics","health","status","supervisor","file","process","pty","probe","ports","mounts","computer_use","browser"],"providers":[{"name":"computer_use","available":true,"capabilities":["computer_use"]},{"name":"browser","available":true,"capabilities":["browser"]}]}`)
		case "/diagnostics":
			if r.URL.Query().Get("detail") != "full" {
				fmt.Fprint(w, `{"protocolVersion":1,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1.5,"socketPath":"/mnt/axern-sandboxd.sock","userProcess":{"state":"running","pid":11}},"capabilities":["archive","diagnostics","health","status","supervisor","file","process","pty","probe","ports","mounts","computer_use","browser"],"providers":[{"name":"computer_use","available":true,"capabilities":["computer_use"]},{"name":"browser","available":true,"capabilities":["browser"]}],"providerSummary":{"total":2,"available":2}}`)
				return
			}
			fmt.Fprint(w, `{"protocolVersion":1,"ready":true,"detail":"full","status":{"daemonPid":7,"uptimeSeconds":1.5,"socketPath":"/mnt/axern-sandboxd.sock","userProcess":{"state":"running","pid":11}},"capabilities":["archive","diagnostics","health","status","supervisor","file","process","pty","probe","ports","mounts","computer_use","browser"],"providers":[{"name":"computer_use","available":true,"capabilities":["computer_use"]},{"name":"browser","available":true,"capabilities":["browser"]}],"providerSummary":{"total":2,"available":2},"processes":{"processes":[{"id":"proc-1","state":"running","pid":12}]},"fileLimits":{"maxArchiveEntries":256,"maxArchiveBytes":67108864,"maxArchiveEntryBytes":33554432,"maxArchivePathDepth":64},"ports":{"ports":[{"protocol":"tcp","address":"127.0.0.1","port":18081,"state":"0A"}]},"mounts":{"mounts":[{"mountpoint":"/","fsType":"overlay"}],"paths":[{"path":"/","exists":true}]},"computerUse":{"available":true,"display":":99","backend":"x11"},"browser":{"available":true,"command":"chromium","running":true,"pid":99,"url":"https://example.com"}}`)
		case "/computer-use/status":
			fmt.Fprint(w, `{"available":true,"display":":99","backend":"x11"}`)
		case "/computer-use/screenshot":
			if r.URL.Query().Get("format") != "jpeg" || r.URL.Query().Get("quality") != "75" || r.URL.Query().Get("width") != "3" {
				t.Fatalf("screenshot query = %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "png-data")
		case "/computer-use/display":
			fmt.Fprint(w, `{"display":":99","backend":"x11","width":1280,"height":720}`)
		case "/computer-use/mouse":
			if r.Method != http.MethodPost {
				t.Fatalf("computer-use mouse method = %s", r.Method)
			}
			var request ComputerUseMouseRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode mouse request: %v", err)
			}
			if request.X != 7 || request.Y != 9 || request.Button != "1" {
				t.Fatalf("mouse request = %#v", request)
			}
			fmt.Fprint(w, `{"ok":true}`)
		case "/computer-use/keyboard":
			if r.Method != http.MethodPost {
				t.Fatalf("computer-use keyboard method = %s", r.Method)
			}
			var request ComputerUseKeyboardRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode keyboard request: %v", err)
			}
			if request.Text != "hello" {
				t.Fatalf("keyboard request = %#v", request)
			}
			fmt.Fprint(w, `{"ok":true}`)
		case "/browser/status":
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":false}`)
		case "/probe":
			if r.Method != http.MethodPost {
				t.Fatalf("probe method = %s", r.Method)
			}
			fmt.Fprint(w, `{"ok":true,"kind":"http","target":"http://127.0.0.1:18081/"}`)
		case "/ports":
			fmt.Fprint(w, `{"ports":[{"protocol":"tcp","address":"127.0.0.1","port":18081,"state":"0A"}]}`)
		case "/mounts":
			fmt.Fprint(w, `{"mounts":[{"mountpoint":"/","fsType":"overlay"}],"paths":[{"path":"/","exists":true}]}`)
		case "/browser/open":
			if r.Method != http.MethodPost {
				t.Fatalf("browser open method = %s", r.Method)
			}
			var request BrowserOpenRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode browser open request: %v", err)
			}
			if request.URL != "https://example.com" {
				t.Fatalf("browser open request = %#v", request)
			}
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":99}`)
		case "/browser/close":
			if r.Method != http.MethodPost {
				t.Fatalf("browser close method = %s", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read browser close body: %v", err)
			}
			if len(body) != 0 {
				t.Fatalf("browser close body = %q, want empty", string(body))
			}
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":false}`)
		case "/browser/navigate":
			if r.Method != http.MethodPost {
				t.Fatalf("browser navigate method = %s", r.Method)
			}
			var request BrowserNavigateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode browser navigate request: %v", err)
			}
			if request.URL != "https://example.org" {
				t.Fatalf("browser navigate request = %#v", request)
			}
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":99,"url":"https://example.org"}`)
		case "/browser/resize":
			var request BrowserResizeRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode browser resize request: %v", err)
			}
			if request.Width != 800 || request.Height != 600 {
				t.Fatalf("browser resize request = %#v", request)
			}
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":99}`)
		case "/browser/click":
			var request BrowserClickRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode browser click request: %v", err)
			}
			if request.X != 10 || request.Y != 20 || request.Button != "1" {
				t.Fatalf("browser click request = %#v", request)
			}
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":99}`)
		case "/browser/type":
			var request BrowserTypeRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode browser type request: %v", err)
			}
			if request.Text != "hello" || request.DelayMS != 1 {
				t.Fatalf("browser type request = %#v", request)
			}
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":99}`)
		case "/browser/wait":
			var request BrowserWaitRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode browser wait request: %v", err)
			}
			if request.TimeoutMS != 25 {
				t.Fatalf("browser wait request = %#v", request)
			}
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":99}`)
		case "/files/stat":
			if r.URL.Query().Get("path") != "/tmp/message.txt" {
				t.Fatalf("stat path = %q", r.URL.Query().Get("path"))
			}
			fmt.Fprint(w, `{"info":{"path":"/tmp/message.txt","kind":1,"size":5,"mode":420,"mtimeNs":7}}`)
		case "/files/list":
			fmt.Fprint(w, `{"entries":[{"path":"/tmp/message.txt","kind":1,"size":5,"mode":420,"mtimeNs":7}]}`)
		case "/files/read":
			fmt.Fprint(w, `{"data":"aGVsbG8="}`)
		case "/files/exists":
			fmt.Fprint(w, `{"exists":true}`)
		case "/files/write", "/files/mkdir", "/files/remove", "/files/copy", "/files/move", "/files/chmod", "/files/touch":
			if r.Method != http.MethodPost {
				t.Fatalf("file mutation method = %s", r.Method)
			}
			fmt.Fprint(w, `{"ok":true}`)
		case "/files/archive/upload":
			if r.Method != http.MethodPost {
				t.Fatalf("archive upload method = %s", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read archive upload: %v", err)
			}
			if string(body) != "tar-data" || r.URL.Query().Get("path") != "/tmp/tree" {
				t.Fatalf("archive upload body=%q query=%s", string(body), r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"ok":true}`)
		case "/files/archive/download":
			if r.URL.Query().Get("path") != "/tmp/tree" {
				t.Fatalf("archive download query=%s", r.URL.RawQuery)
			}
			fmt.Fprint(w, "tar-data")
		default:
			http.NotFound(w, r)
		}
	}))
	defer shutdown()

	client := NewClient(socketPath)
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != "ok" {
		t.Fatalf("health status = %q, want ok", health.Status)
	}
	if health.ProtocolVersion != wire.ProtocolVersion {
		t.Fatalf("health protocol version = %d, want %d", health.ProtocolVersion, wire.ProtocolVersion)
	}
	snapshot, err := client.WaitReady(context.Background(), time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}
	if !snapshot.Ready.Ready || snapshot.Status.UserProcess.State != "running" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Ready.ProtocolVersion != wire.ProtocolVersion {
		t.Fatalf("ready snapshot protocol version = %d, want %d", snapshot.Ready.ProtocolVersion, wire.ProtocolVersion)
	}
	for _, capability := range []string{"archive", "diagnostics", "health", "status", "supervisor", "file", "process", "pty", "probe", "ports", "mounts", "computer_use", "browser"} {
		if !hasCapability(snapshot.Capabilities.Capabilities, capability) {
			t.Fatalf("capabilities missing %q in %#v", capability, snapshot.Capabilities.Capabilities)
		}
	}
	if snapshot.Capabilities.ProtocolVersion != 1 || len(snapshot.Capabilities.Providers) != 2 || !snapshot.Capabilities.Providers[0].Available {
		t.Fatalf("capability providers = %#v", snapshot.Capabilities)
	}
	diagnostics, err := client.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("Diagnostics() error = %v", err)
	}
	if diagnostics.ProtocolVersion != 1 || !diagnostics.Ready || diagnostics.Status.UserProcess.State != "running" {
		t.Fatalf("diagnostics status = %#v", diagnostics)
	}
	if !hasCapability(diagnostics.Capabilities, "archive") || len(diagnostics.Providers) != 2 || diagnostics.Providers[0].Name != "computer_use" {
		t.Fatalf("diagnostics providers = %#v", diagnostics)
	}
	if diagnostics.Processes == nil || len(diagnostics.Processes.Processes) != 1 || diagnostics.Processes.Processes[0].ID != "proc-1" {
		t.Fatalf("diagnostics processes = %#v", diagnostics.Processes)
	}
	if diagnostics.ComputerUse == nil || diagnostics.ComputerUse.Display != ":99" || diagnostics.Browser == nil || !diagnostics.Browser.Running {
		t.Fatalf("diagnostics session status = %#v", diagnostics)
	}
	if diagnostics.FileLimits == nil || diagnostics.FileLimits.MaxArchiveEntries != 256 {
		t.Fatalf("diagnostics file limits = %#v", diagnostics.FileLimits)
	}
	if diagnostics.Ports == nil || len(diagnostics.Ports.Ports) != 1 || diagnostics.Mounts == nil || len(diagnostics.Mounts.Paths) != 1 {
		t.Fatalf("diagnostics full details = %#v", diagnostics)
	}
	summary, err := client.DiagnosticsSummary(context.Background())
	if err != nil {
		t.Fatalf("DiagnosticsSummary() error = %v", err)
	}
	if summary.Detail == "full" || summary.Processes != nil || summary.Ports != nil || summary.Mounts != nil {
		t.Fatalf("summary diagnostics = %#v", summary)
	}
	probe, err := client.Probe(context.Background(), wire.ProbeRequest{HTTP: &wire.HTTPProbe{Port: 18081, Path: "/"}})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !probe.OK || probe.Kind != "http" {
		t.Fatalf("probe = %#v", probe)
	}
	ports, err := client.Ports(context.Background())
	if err != nil {
		t.Fatalf("Ports() error = %v", err)
	}
	if len(ports.Ports) != 1 || ports.Ports[0].Port != 18081 {
		t.Fatalf("ports = %#v", ports)
	}
	mounts, err := client.Mounts(context.Background())
	if err != nil {
		t.Fatalf("Mounts() error = %v", err)
	}
	if len(mounts.Paths) != 1 || !mounts.Paths[0].Exists {
		t.Fatalf("mounts = %#v", mounts)
	}
	computerStatus, err := client.ComputerUseStatus(context.Background())
	if err != nil {
		t.Fatalf("ComputerUseStatus() error = %v", err)
	}
	if !computerStatus.Available || computerStatus.Display != ":99" {
		t.Fatalf("computer status = %#v", computerStatus)
	}
	screenshot, err := client.ComputerUseScreenshot(context.Background(), ComputerUseScreenshotRequest{
		Region:  &ComputerUseRegion{X: 1, Y: 2, Width: 3, Height: 4},
		Format:  "jpeg",
		Quality: 75,
	})
	if err != nil {
		t.Fatalf("ComputerUseScreenshot() error = %v", err)
	}
	if string(screenshot.Data) != "png-data" {
		t.Fatalf("screenshot = %q", string(screenshot.Data))
	}
	if screenshot.ContentType != "image/png" {
		t.Fatalf("screenshot content type = %q, want image/png", screenshot.ContentType)
	}
	display, err := client.ComputerUseDisplay(context.Background())
	if err != nil {
		t.Fatalf("ComputerUseDisplay() error = %v", err)
	}
	if display.Width != 1280 || display.Height != 720 {
		t.Fatalf("display = %#v", display)
	}
	if err := client.ComputerUseMouse(context.Background(), ComputerUseMouseRequest{X: 7, Y: 9, Button: "1"}); err != nil {
		t.Fatalf("ComputerUseMouse() error = %v", err)
	}
	if err := client.ComputerUseKeyboard(context.Background(), ComputerUseKeyboardRequest{Text: "hello"}); err != nil {
		t.Fatalf("ComputerUseKeyboard() error = %v", err)
	}
	browserStatus, err := client.BrowserStatus(context.Background())
	if err != nil {
		t.Fatalf("BrowserStatus() error = %v", err)
	}
	if !browserStatus.Available || browserStatus.Running {
		t.Fatalf("browser status = %#v", browserStatus)
	}
	browserStatus, err = client.BrowserOpen(context.Background(), BrowserOpenRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("BrowserOpen() error = %v", err)
	}
	if !browserStatus.Running || browserStatus.Pid != 99 {
		t.Fatalf("browser open status = %#v", browserStatus)
	}
	browserStatus, err = client.BrowserClose(context.Background())
	if err != nil {
		t.Fatalf("BrowserClose() error = %v", err)
	}
	if browserStatus.Running {
		t.Fatalf("browser close status = %#v", browserStatus)
	}
	browserStatus, err = client.BrowserNavigate(context.Background(), BrowserNavigateRequest{URL: "https://example.org"})
	if err != nil {
		t.Fatalf("BrowserNavigate() error = %v", err)
	}
	if !browserStatus.Running || browserStatus.URL != "https://example.org" {
		t.Fatalf("browser navigate status = %#v", browserStatus)
	}
	if _, err := client.BrowserResize(context.Background(), BrowserResizeRequest{Width: 800, Height: 600}); err != nil {
		t.Fatalf("BrowserResize() error = %v", err)
	}
	if _, err := client.BrowserClick(context.Background(), BrowserClickRequest{X: 10, Y: 20, Button: "1"}); err != nil {
		t.Fatalf("BrowserClick() error = %v", err)
	}
	if _, err := client.BrowserType(context.Background(), BrowserTypeRequest{Text: "hello", DelayMS: 1}); err != nil {
		t.Fatalf("BrowserType() error = %v", err)
	}
	if _, err := client.BrowserWait(context.Background(), BrowserWaitRequest{TimeoutMS: 25}); err != nil {
		t.Fatalf("BrowserWait() error = %v", err)
	}
	stat, err := client.StatFile(context.Background(), "/tmp/message.txt")
	if err != nil {
		t.Fatalf("StatFile() error = %v", err)
	}
	if stat.Info.GetPath() != "/tmp/message.txt" || stat.Info.GetSize() != 5 {
		t.Fatalf("stat = %#v", stat)
	}
	list, err := client.ListDir(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("ListDir() error = %v", err)
	}
	if len(list.Entries) != 1 || list.Entries[0].GetPath() != "/tmp/message.txt" {
		t.Fatalf("list = %#v", list)
	}
	read, err := client.ReadFile(context.Background(), "/tmp/message.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(read.Data) != "hello" {
		t.Fatalf("read = %#v", read)
	}
	exists, err := client.Exists(context.Background(), "/tmp/message.txt")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists.Exists {
		t.Fatalf("exists = %#v", exists)
	}
	if err := client.WriteFile(context.Background(), FileWriteRequest{Path: "/tmp/message.txt", Data: []byte("hello")}); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := client.Mkdir(context.Background(), FileMkdirRequest{Path: "/tmp/dir", Parents: true}); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := client.Remove(context.Background(), FileRemoveRequest{Path: "/tmp/dir", Recursive: true}); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := client.Copy(context.Background(), FileCopyRequest{SrcPath: "/tmp/a", DstPath: "/tmp/b", Overwrite: true}); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if err := client.Move(context.Background(), FileMoveRequest{SrcPath: "/tmp/b", DstPath: "/tmp/c", Overwrite: true}); err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if err := client.Chmod(context.Background(), FileChmodRequest{Path: "/tmp/c", Mode: 0640}); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if err := client.Touch(context.Background(), FileTouchRequest{Path: "/tmp/c", Create: true}); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	if err := client.UploadArchive(context.Background(), FileArchiveUploadRequest{Path: "/tmp/tree"}, strings.NewReader("tar-data")); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}
	var archive bytes.Buffer
	if err := client.DownloadArchive(context.Background(), FileArchiveDownloadRequest{Path: "/tmp/tree"}, &archive); err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	if archive.String() != "tar-data" {
		t.Fatalf("archive = %q", archive.String())
	}
}

func TestClientParsesStructuredSandboxdErrors(t *testing.T) {
	socketPath, shutdown := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"code":"invalid_argument","message":"bad browser request"}}`)
	}))
	defer shutdown()

	client := NewClient(socketPath)
	_, err := client.BrowserNavigate(context.Background(), BrowserNavigateRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error")
	}
	if StatusCode(err) != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", StatusCode(err), http.StatusBadRequest)
	}
	if ErrorCode(err) != "invalid_argument" {
		t.Fatalf("error code = %q, want invalid_argument", ErrorCode(err))
	}
	if ClassifyError(err) != ErrorClassInvalidArgument {
		t.Fatalf("error class = %q, want %q", ClassifyError(err), ErrorClassInvalidArgument)
	}
	if !strings.Contains(err.Error(), "bad browser request") {
		t.Fatalf("error = %v", err)
	}
}

func TestClassifySandboxdErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{name: "not found code", err: &StatusError{Code: ErrorCodeNotFound, StatusCode: http.StatusServiceUnavailable}, want: ErrorClassNotFound},
		{name: "not found status", err: &StatusError{StatusCode: http.StatusNotFound}, want: ErrorClassNotFound},
		{name: "invalid status", err: &StatusError{StatusCode: http.StatusBadRequest}, want: ErrorClassInvalidArgument},
		{name: "already exists code", err: &StatusError{Code: ErrorCodeAlreadyExists, StatusCode: http.StatusServiceUnavailable}, want: ErrorClassAlreadyExists},
		{name: "already exists status", err: &StatusError{StatusCode: http.StatusConflict}, want: ErrorClassAlreadyExists},
		{name: "failed precondition code", err: &StatusError{Code: ErrorCodeFailedCondition, StatusCode: http.StatusOK}, want: ErrorClassFailedCondition},
		{name: "unavailable code", err: &StatusError{Code: ErrorCodeUnavailable, StatusCode: http.StatusOK}, want: ErrorClassUnavailable},
		{name: "unavailable status", err: &StatusError{StatusCode: http.StatusServiceUnavailable}, want: ErrorClassUnavailable},
		{name: "command failed code", err: &StatusError{Code: ErrorCodeCommandFailed, StatusCode: http.StatusOK}, want: ErrorClassCommandFailed},
		{name: "timeout code", err: &StatusError{Code: ErrorCodeTimeout, StatusCode: http.StatusOK}, want: ErrorClassTimeout},
		{name: "timeout status", err: &StatusError{StatusCode: http.StatusRequestTimeout}, want: ErrorClassTimeout},
		{name: "method not allowed code", err: &StatusError{Code: ErrorCodeMethodNotAllowed, StatusCode: http.StatusOK}, want: ErrorClassMethodNotAllowed},
		{name: "method not allowed status", err: &StatusError{StatusCode: http.StatusMethodNotAllowed}, want: ErrorClassMethodNotAllowed},
		{name: "internal code", err: &StatusError{Code: ErrorCodeInternal, StatusCode: http.StatusOK}, want: ErrorClassInternal},
		{name: "internal status", err: &StatusError{StatusCode: http.StatusInternalServerError}, want: ErrorClassInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != tt.want {
				t.Fatalf("ClassifyError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOperationErrorMapsSandboxdClasses(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "invalid", err: &StatusError{Code: ErrorCodeInvalidArgument}, want: errord.ErrInvalidArgument},
		{name: "not found", err: &StatusError{Code: ErrorCodeNotFound}, want: errord.ErrNotFound},
		{name: "already exists", err: &StatusError{Code: ErrorCodeAlreadyExists}, want: errord.ErrAlreadyExists},
		{name: "unavailable", err: &StatusError{Code: ErrorCodeUnavailable}, want: errord.ErrFailedPrecondition},
		{name: "timeout", err: &StatusError{Code: ErrorCodeTimeout}, want: errord.ErrFailedPrecondition},
		{name: "method not allowed", err: &StatusError{Code: ErrorCodeMethodNotAllowed}, want: errord.ErrFailedPrecondition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := OperationError("process", "start", tt.err)
			if !errors.Is(err, tt.want) {
				t.Fatalf("OperationError() = %v, want %v", err, tt.want)
			}
		})
	}
}

func hasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func TestClientWaitReadyRetriesUntilReady(t *testing.T) {
	attempts := 0
	socketPath, shutdown := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/diagnostics":
			attempts++
			if attempts < 3 {
				fmt.Fprint(w, `{"ready":false}`)
				return
			}
			fmt.Fprint(w, `{"protocolVersion":1,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1,"socketPath":"/mnt/axern-sandboxd.sock","userProcess":{"state":"exited"}},"capabilities":["health"]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer shutdown()

	snapshot, err := NewClient(socketPath).WaitReady(context.Background(), time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}
	if snapshot.Status.UserProcess.State != "exited" {
		t.Fatalf("user state = %q, want exited", snapshot.Status.UserProcess.State)
	}
	if attempts < 3 {
		t.Fatalf("attempts = %d, want at least 3", attempts)
	}
}

func TestClientWaitReadyAcceptsTerminalUserStates(t *testing.T) {
	for _, state := range []string{"starting", "running", "exited", "failed", "not_started"} {
		t.Run(state, func(t *testing.T) {
			socketPath, shutdown := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/diagnostics" {
					http.NotFound(w, r)
					return
				}
				fmt.Fprintf(w, `{"protocolVersion":1,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1,"socketPath":"/mnt/axern-sandboxd.sock","userProcess":{"state":%q}},"capabilities":["health","status","supervisor"]}`, state)
			}))
			defer shutdown()

			snapshot, err := NewClient(socketPath).WaitReady(context.Background(), time.Second, time.Millisecond)
			if err != nil {
				t.Fatalf("WaitReady() error = %v", err)
			}
			if snapshot.Status.UserProcess.State != state {
				t.Fatalf("user state = %q, want %q", snapshot.Status.UserProcess.State, state)
			}
		})
	}
}

func TestClientWaitReadyRejectsProtocolMismatch(t *testing.T) {
	socketPath, shutdown := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"protocolVersion":2,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1,"socketPath":"/mnt/axern-sandboxd.sock","userProcess":{"state":"running"}},"capabilities":["health"]}`)
	}))
	defer shutdown()

	_, err := NewClient(socketPath).WaitReady(context.Background(), 25*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("WaitReady() error = nil, want protocol mismatch")
	}
	if !strings.Contains(err.Error(), "protocol version = 2, want 1") {
		t.Fatalf("error = %v, want protocol mismatch detail", err)
	}
}

func TestClientWaitReadyTimeoutAndInvalidJSON(t *testing.T) {
	socketPath, shutdown := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not-json`)
	}))
	defer shutdown()

	_, err := NewClient(socketPath).WaitReady(context.Background(), 25*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("WaitReady() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "decode sandboxd /diagnostics response") {
		t.Fatalf("error = %v, want invalid json detail", err)
	}
}

func TestClientWaitReadySocketMissingAndContextCancel(t *testing.T) {
	_, err := NewClient(filepath.Join(t.TempDir(), "missing.sock")).WaitReady(context.Background(), 25*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("WaitReady() missing socket error = nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewClient(filepath.Join(t.TempDir(), "missing.sock")).WaitReady(ctx, time.Second, time.Millisecond)
	if err == nil {
		t.Fatal("WaitReady() canceled context error = nil")
	}
}

func TestEnrichLabels(t *testing.T) {
	labels := EnrichLabels(map[string]string{"existing": "true"}, "/tmp/sandboxd.sock", wire.ReadySnapshot{
		Capabilities: wire.CapabilitiesResponse{Capabilities: []string{"health", "status"}},
		Status:       wire.StatusResponse{UserProcess: wire.UserProcessStatus{State: "running"}},
	})
	if labels["existing"] != "true" || labels[LabelReady] != "true" || labels[LabelSocket] != "/tmp/sandboxd.sock" {
		t.Fatalf("labels = %#v", labels)
	}
	if labels[LabelCapabilities] != "health,status" || labels[LabelUserState] != "running" {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestShortUnixSocketPathUsesSymlinkForLongPath(t *testing.T) {
	longPath := "/" + strings.Repeat("very-long/", 16) + "sandboxd.sock"
	dialPath, cleanup, err := shortUnixSocketPath(longPath)
	if err != nil {
		t.Fatalf("shortUnixSocketPath() error = %v", err)
	}
	defer cleanup()
	if len(dialPath) >= len(longPath) {
		t.Fatalf("dial path %q was not shortened from %q", dialPath, longPath)
	}
	target, err := os.Readlink(dialPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != longPath {
		t.Fatalf("target = %q, want %q", target, longPath)
	}
}

func serveUnix(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "sd-*")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(done)
	}()
	return socketPath, func() {
		_ = server.Close()
		<-done
		_ = os.RemoveAll(dir)
	}
}
