package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/imageprocess"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type imageProcessTestMounter struct {
	path        string
	mu          sync.Mutex
	umountCount int
}

func (m *imageProcessTestMounter) Resolve(cfg langrtmanager.RootfsConfig) (langrtmanager.RootfsConfig, error) {
	return cfg, nil
}

func (m *imageProcessTestMounter) Reconcile([]string) error { return nil }

func (m *imageProcessTestMounter) Mount(cfg langrtmanager.RootfsConfig) (*langrtmanager.MountResult, error) {
	if m.path == "" {
		return nil, fmt.Errorf("test rootfs path is empty")
	}
	return &langrtmanager.MountResult{Path: m.path, Env: []string{"IMAGE_ENV=1"}}, nil
}

func (m *imageProcessTestMounter) Umount(langrtmanager.RootfsConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.umountCount++
	return nil
}

func (m *imageProcessTestMounter) UmountCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.umountCount
}

func TestExecImageCreatesTransientImageContainerAndCleansUp(t *testing.T) {
	hostDir := t.TempDir()
	rootfsDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(hostDir, "project"), 0755))
	session := &execSessionStub{
		chunks: []contract.Chunk{{Stdout: []byte("image-ok\n")}},
		exit:   contract.Exit{Status: 7},
	}
	handler := &runtimeSpyHandler{
		name:        "runsc",
		execSession: session,
		createMetadataLabels: map[string]string{
			runtimesandboxd.LabelReady:        "true",
			runtimesandboxd.LabelSocket:       "/tmp/sandboxd.sock",
			runtimesandboxd.LabelCapabilities: "process,pty",
			runtimesandboxd.LabelUserState:    "running",
		},
		containerSpec: &specs.Spec{Mounts: []specs.Mount{{
			Type:        "bind",
			Source:      hostDir,
			Destination: "/workspace",
		}}},
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	s.lrtManager = langrtmanager.NewLanguageRuntimeManager(&imageProcessTestMounter{path: rootfsDir})
	s.configureAllocationController()
	storeRunningExecContainer(t, s, "runsc", "axctl-task-image")

	resp, err := s.ExecImage(context.Background(), &apipb.ExecImageRequest{
		ID: "axctl-task-image",
		Spec: &apipb.ImageProcessSpec{
			Image:   "ghcr.io/cofy-x/agent:latest",
			Command: []string{"tool", "run"},
			Cwd:     "/workspace",
			Env:     map[string]string{"A": "B"},
			Mounts: []*apipb.ImageProcessMount{{
				SandboxPath: "/workspace/project",
				TargetPath:  "/workspace",
			}},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, int32(7), resp.GetExitCode())
	assert.Equal(t, []byte("image-ok\n"), resp.GetStdout())
	assert.Equal(t, 1, handler.createCalls)
	assert.Equal(t, 1, handler.deleteCalls)
	assert.Nil(t, handler.lastExecRequest, "ExecImage must not call sandbox ExecContainer")
	assert.True(t, strings.HasPrefix(handler.lastRequest.GetID(), "axctl-imageproc-"))
	assert.Equal(t, "runsc", handler.lastRequest.GetRuntime())
	assert.Equal(t, rootfsDir, handler.lastRequest.GetRootfs().GetRootDir())
	assert.Equal(t, "ghcr.io/cofy-x/agent:latest", handler.lastRequest.GetLabels()[imageprocess.ImageLabel])
	assert.Equal(t, "axctl-task-image", handler.lastRequest.GetLabels()[imageprocess.ParentAllocationLabel])
	assert.Equal(t, imageprocess.Kind, handler.lastRequest.GetLabels()[imageprocess.KindLabel])
	require.Len(t, handler.lastRequest.GetMounts(), 1)
	assert.Equal(t, filepath.Join(hostDir, "project"), handler.lastRequest.GetMounts()[0].GetSource())
	assert.Equal(t, "/workspace", handler.lastRequest.GetMounts()[0].GetTarget())
	assert.DirExists(t, filepath.Join(rootfsDir, "workspace"))
	assert.Equal(t, handler.lastRequest.GetID(), handler.lastProcessOptions.ContainerID)
	assert.Equal(t, "/tmp/sandboxd.sock", handler.lastProcessOptions.ContainerLabels[runtimesandboxd.LabelSocket])
	assert.Contains(t, handler.lastProcessOptions.ContainerLabels[runtimesandboxd.LabelCapabilities], "process")
	assert.Equal(t, []string{"tool", "run"}, handler.lastSessionOpen.GetCommand())
	assert.Equal(t, "/workspace", handler.lastSessionOpen.GetCwd())
	assert.Equal(t, "B", handler.lastSessionOpen.GetEnvs()[0].GetValue())
	assert.True(t, session.stdinClosed)
	assert.True(t, session.closed)
}

func TestEnsureImageProcessRuntimeDisablesExecutionEnvelope(t *testing.T) {
	rootfsDir := t.TempDir()
	handler := &runtimeSpyHandler{name: "runsc"}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	s.lrtManager = langrtmanager.NewLanguageRuntimeManager(&imageProcessTestMounter{path: rootfsDir})
	s.configureAllocationController()

	lrt, err := s.ensureImageProcessRuntime(t.Context(), imageprocess.RuntimeTemplate("runsc", "ghcr.io/cofy-x/agent:latest"))
	require.NoError(t, err)
	assert.False(t, lrt.ExecutionEnvelopeEnabled())
}

func TestExecImageCleansUpTransientContainerWhenProcessOpenFails(t *testing.T) {
	hostDir := t.TempDir()
	rootfsDir := t.TempDir()
	require.NoError(t, os.MkdirAll(hostDir, 0755))
	handler := &runtimeSpyHandler{
		name:           "runsc",
		execSessionErr: io.ErrUnexpectedEOF,
		containerSpec: &specs.Spec{Mounts: []specs.Mount{{
			Type:        "bind",
			Source:      hostDir,
			Destination: "/workspace",
		}}},
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	s.lrtManager = langrtmanager.NewLanguageRuntimeManager(&imageProcessTestMounter{path: rootfsDir})
	s.configureAllocationController()
	storeRunningExecContainer(t, s, "runsc", "axctl-task-open-fails")

	_, err := s.ExecImage(context.Background(), &apipb.ExecImageRequest{
		ID: "axctl-task-open-fails",
		Spec: &apipb.ImageProcessSpec{
			Image:   "ghcr.io/cofy-x/agent:latest",
			Command: []string{"tool", "run"},
			Mounts: []*apipb.ImageProcessMount{{
				SandboxPath: "/workspace",
				TargetPath:  "/workspace",
			}},
		},
	})

	assert.Error(t, err)
	assert.Equal(t, 1, handler.createCalls)
	assert.Equal(t, 1, handler.deleteCalls)
}

func TestExecImageCleansUpTransientContainerWhenCloseStdinFails(t *testing.T) {
	hostDir := t.TempDir()
	rootfsDir := t.TempDir()
	require.NoError(t, os.MkdirAll(hostDir, 0755))
	session := &execSessionStub{closeStdinErr: io.ErrClosedPipe}
	handler := &runtimeSpyHandler{
		name:        "runsc",
		execSession: session,
		containerSpec: &specs.Spec{Mounts: []specs.Mount{{
			Type:        "bind",
			Source:      hostDir,
			Destination: "/workspace",
		}}},
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	s.lrtManager = langrtmanager.NewLanguageRuntimeManager(&imageProcessTestMounter{path: rootfsDir})
	s.configureAllocationController()
	storeRunningExecContainer(t, s, "runsc", "axctl-task-stdin-fails")

	_, err := s.ExecImage(context.Background(), &apipb.ExecImageRequest{
		ID: "axctl-task-stdin-fails",
		Spec: &apipb.ImageProcessSpec{
			Image:   "ghcr.io/cofy-x/agent:latest",
			Command: []string{"tool", "run"},
			Mounts: []*apipb.ImageProcessMount{{
				SandboxPath: "/workspace",
				TargetPath:  "/workspace",
			}},
		},
	})

	assert.Error(t, err)
	assert.True(t, session.closed)
	assert.Equal(t, 1, handler.createCalls)
	assert.Equal(t, 1, handler.deleteCalls)
}

func TestProcessImageCleansUpTransientContainerAndRootfsWhenStreamCloses(t *testing.T) {
	hostDir := t.TempDir()
	rootfsDir := t.TempDir()
	require.NoError(t, os.MkdirAll(hostDir, 0755))
	mounter := &imageProcessTestMounter{path: rootfsDir}
	session := &execSessionStub{exit: contract.Exit{Status: 0}}
	handler := &runtimeSpyHandler{
		name:        "runsc",
		execSession: session,
		createMetadataLabels: map[string]string{
			runtimesandboxd.LabelReady:        "true",
			runtimesandboxd.LabelSocket:       "/tmp/sandboxd.sock",
			runtimesandboxd.LabelCapabilities: "process",
			runtimesandboxd.LabelUserState:    "running",
		},
		containerSpec: &specs.Spec{Mounts: []specs.Mount{{
			Type:        "bind",
			Source:      hostDir,
			Destination: "/workspace",
		}}},
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	s.lrtManager = langrtmanager.NewLanguageRuntimeManager(mounter)
	s.configureAllocationController()
	storeRunningExecContainer(t, s, "runsc", "axctl-task-stream-close")

	stream := &imageProcessStreamStub{requests: []*apipb.ProcessImageRequest{{
		Payload: &apipb.ProcessImageRequest_Open{Open: &apipb.ProcessImageOpen{
			ID: "axctl-task-stream-close",
			Spec: &apipb.ImageProcessSpec{
				Image:   "ghcr.io/cofy-x/agent:latest",
				Command: []string{"cat"},
				Mounts: []*apipb.ImageProcessMount{{
					SandboxPath: "/workspace",
					TargetPath:  "/workspace",
				}},
			},
		}},
	}}}

	require.NoError(t, s.ProcessImage(stream))
	assert.Equal(t, 1, handler.createCalls)
	assert.Equal(t, 1, handler.deleteCalls)
	assert.Equal(t, 1, mounter.UmountCount())
	assert.True(t, session.stdinClosed)
	assert.True(t, session.closed)
	require.Len(t, stream.sent, 2)
	assert.NotNil(t, stream.sent[0].GetReady())
	assert.NotNil(t, stream.sent[1].GetExit())
}

func TestExecImageRejectsInvalidSpecAndStoppedAllocation(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": &runtimeSpyHandler{name: "runsc"},
	})

	_, err := s.ExecImage(context.Background(), &apipb.ExecImageRequest{
		ID: "axctl-task",
		Spec: &apipb.ImageProcessSpec{
			Command: []string{"tool"},
		},
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	storeExitedExecContainer(t, s, "runsc", "axctl-task-exited")
	_, err = s.ExecImage(context.Background(), &apipb.ExecImageRequest{
		ID: "axctl-task-exited",
		Spec: &apipb.ImageProcessSpec{
			Image:   "ghcr.io/cofy-x/agent:latest",
			Command: []string{"tool"},
		},
	})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

type imageProcessStreamStub struct {
	ctx      context.Context
	requests []*apipb.ProcessImageRequest
	sent     []*apipb.ProcessImageResponse
}

func (s *imageProcessStreamStub) Recv() (*apipb.ProcessImageRequest, error) {
	if len(s.requests) == 0 {
		return nil, io.EOF
	}
	req := s.requests[0]
	s.requests = s.requests[1:]
	return req, nil
}

func (s *imageProcessStreamStub) Send(response *apipb.ProcessImageResponse) error {
	s.sent = append(s.sent, response)
	return nil
}

func (s *imageProcessStreamStub) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
