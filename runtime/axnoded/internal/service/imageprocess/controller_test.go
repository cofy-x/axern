package imageprocess

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControllerExecImageCreatesActorAndCollectsOutput(t *testing.T) {
	hostDir := t.TempDir()
	rootfsDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(hostDir, "project"), 0755))
	handler := &controllerHandler{
		FakeRuntimeHandler: runtimetest.NewFakeRuntimeHandler(),
		session: &controllerSession{
			chunks: []contract.Chunk{{Stdout: []byte("ok\n")}},
			exit:   contract.Exit{Status: 4},
		},
	}
	var createdID string
	var deletedID string
	parentNetworkKey := resourcemanager.ResourceAnnotationKeyPrefix + string(resourcemanager.InterfaceResourceName)
	parentNetworkValue := "parent-label-net-resource"
	controller := NewController(Options{
		InspectTarget: func(context.Context, string) (Target, contract.RuntimeHandler, error) {
			return Target{
				Runtime: "runsc",
				Spec: &specs.Spec{
					Annotations: map[string]string{parentNetworkKey: "parent-spec-net-resource"},
					Mounts: []specs.Mount{{
						Type:        "bind",
						Source:      hostDir,
						Destination: "/workspace",
					}},
				},
				Labels: map[string]string{parentNetworkKey: parentNetworkValue},
			}, handler, nil
		},
		EnsureRuntime: func(_ context.Context, template *runtime.RuntimeTemplate) (*langrtmanager.LanguageRuntime, error) {
			assert.Equal(t, "ghcr.io/cofy-x/agent:latest", template.GetRootfs().GetImageUrl())
			return testLanguageRuntime(t, rootfsDir), nil
		},
		CreateContainer: func(_ context.Context, _ *langrtmanager.LanguageRuntime, _, createRequest *runtime.CreateContainerRequest) error {
			createdID = createRequest.GetID()
			assert.Equal(t, "runsc", createRequest.GetRuntime())
			assert.Equal(t, rootfsDir, createRequest.GetRootfs().GetRootDir())
			require.Len(t, createRequest.GetMounts(), 1)
			assert.Equal(t, filepath.Join(hostDir, "project"), createRequest.GetMounts()[0].GetSource())
			assert.Empty(t, createRequest.GetLabels()[parentNetworkKey])
			return nil
		},
		LoadContainerLabels: func(actorID string) (map[string]string, error) {
			return map[string]string{"ready": "true", "actor": actorID}, nil
		},
		DeleteContainer: func(_ context.Context, actorID string) error {
			deletedID = actorID
			return nil
		},
	})

	resp, err := controller.ExecImage(context.Background(), &runtime.ExecImageRequest{
		ID: "task-1",
		Spec: &runtime.ImageProcessSpec{
			Image:   "ghcr.io/cofy-x/agent:latest",
			Command: []string{"tool", "run"},
			Env:     map[string]string{"A": "B"},
			Mounts: []*runtime.ImageProcessMount{{
				SandboxPath: "/workspace/project",
				TargetPath:  "/workspace",
			}},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, int32(4), resp.GetExitCode())
	assert.Equal(t, []byte("ok\n"), resp.GetStdout())
	assert.NotEmpty(t, createdID)
	assert.Equal(t, createdID, deletedID)
	assert.Equal(t, createdID, handler.lastOptions.ContainerID)
	assert.Equal(t, map[string]string{"ready": "true", "actor": createdID}, handler.lastOptions.ContainerLabels)
	assert.Equal(t, []string{"tool", "run"}, handler.lastOpen.GetCommand())
	assert.Equal(t, "B", handler.lastOpen.GetEnv()["A"])
	assert.True(t, handler.session.closed)
	assert.True(t, handler.session.stdinClosed)
}

func TestControllerExecImageRejectsInvalidSpec(t *testing.T) {
	controller := NewController(Options{})

	_, err := controller.ExecImage(context.Background(), &runtime.ExecImageRequest{ID: "task-1"})

	assert.ErrorIs(t, err, errord.ErrInvalidArgument)
}

func TestControllerProcessImageSendsReadyAndExit(t *testing.T) {
	session := &controllerSession{
		chunks: []contract.Chunk{{Stdout: []byte("hello")}},
		exit:   contract.Exit{Status: 0},
	}
	handler := &controllerHandler{
		FakeRuntimeHandler: runtimetest.NewFakeRuntimeHandler(),
		session:            session,
	}
	controller := NewController(Options{
		InspectTarget: func(context.Context, string) (Target, contract.RuntimeHandler, error) {
			return Target{Runtime: "runsc", Spec: &specs.Spec{}}, handler, nil
		},
		EnsureRuntime: func(context.Context, *runtime.RuntimeTemplate) (*langrtmanager.LanguageRuntime, error) {
			return testLanguageRuntime(t, t.TempDir()), nil
		},
		CreateContainer: func(context.Context, *langrtmanager.LanguageRuntime, *runtime.CreateContainerRequest, *runtime.CreateContainerRequest) error {
			return nil
		},
		LoadContainerLabels: func(actorID string) (map[string]string, error) {
			return map[string]string{"actor": actorID}, nil
		},
		DeleteContainer: func(context.Context, string) error {
			return nil
		},
	})
	stream := &processImageStreamStub{requests: []*runtime.ProcessImageRequest{{
		Payload: &runtime.ProcessImageRequest_Open{Open: &runtime.ProcessImageOpen{
			ID: "task-1",
			Spec: &runtime.ImageProcessSpec{
				Image:   "ghcr.io/cofy-x/agent:latest",
				Command: []string{"cat"},
			},
		}},
	}}}

	err := controller.ProcessImage(stream)

	require.NoError(t, err)
	require.Len(t, stream.sent, 3)
	assert.NotNil(t, stream.sent[0].GetReady())
	assert.Equal(t, []byte("hello"), stream.sent[1].GetStdout())
	assert.NotNil(t, stream.sent[2].GetExit())
	assert.True(t, session.stdinClosed)
	assert.True(t, session.closed)
}

type controllerHandler struct {
	*runtimetest.FakeRuntimeHandler
	session     *controllerSession
	lastOpen    *runtime.ProcessOpen
	lastOptions contract.HandlerOptions
}

func (h *controllerHandler) ProcessService() contract.ProcessService {
	return controllerProcessService{handler: h}
}

type controllerProcessService struct {
	handler *controllerHandler
}

func (s controllerProcessService) OpenProcess(_ context.Context, request *runtime.ProcessOpen, options contract.HandlerOptions) (contract.Session, error) {
	s.handler.lastOpen = request
	s.handler.lastOptions = options
	return s.handler.session, nil
}

type controllerSession struct {
	chunks      []contract.Chunk
	exit        contract.Exit
	closed      bool
	stdinClosed bool
}

func (s *controllerSession) Write([]byte) error { return nil }

func (s *controllerSession) CloseStdin() error {
	s.stdinClosed = true
	return nil
}

func (s *controllerSession) Resize(uint32, uint32) error { return nil }

func (s *controllerSession) Signal(string) error { return nil }

func (s *controllerSession) Recv() (contract.Chunk, error) {
	if len(s.chunks) == 0 {
		return contract.Chunk{}, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func (s *controllerSession) Wait() (contract.Exit, error) {
	return s.exit, nil
}

func (s *controllerSession) Close() error {
	s.closed = true
	return nil
}

type processImageStreamStub struct {
	ctx      context.Context
	requests []*runtime.ProcessImageRequest
	sent     []*runtime.ProcessImageResponse
}

func (s *processImageStreamStub) Recv() (*runtime.ProcessImageRequest, error) {
	if len(s.requests) == 0 {
		return nil, io.EOF
	}
	request := s.requests[0]
	s.requests = s.requests[1:]
	return request, nil
}

func (s *processImageStreamStub) Send(response *runtime.ProcessImageResponse) error {
	s.sent = append(s.sent, response)
	return nil
}

func (s *processImageStreamStub) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

type controllerMounter struct {
	path string
}

func (m controllerMounter) Resolve(cfg langrtmanager.RootfsConfig) (langrtmanager.RootfsConfig, error) {
	return cfg, nil
}

func (m controllerMounter) Reconcile([]string) error { return nil }

func (m controllerMounter) Mount(cfg langrtmanager.RootfsConfig) (*langrtmanager.MountResult, error) {
	mount, err := langrtmanager.DescribeLocalRootfs(m.path)
	if mount != nil {
		mount.LeaseID = cfg.LeaseID
	}
	return &langrtmanager.MountResult{Path: m.path, ImmutableMount: mount}, err
}

func (m controllerMounter) Umount(langrtmanager.RootfsConfig) error {
	return nil
}

func testLanguageRuntime(t *testing.T, rootfsDir string) *langrtmanager.LanguageRuntime {
	t.Helper()
	rootfs, err := langrtmanager.NewRootFS(langrtmanager.RootfsConfig{}, controllerMounter{path: rootfsDir}, nil)
	require.NoError(t, err)
	return &langrtmanager.LanguageRuntime{
		ID:      "image-process-runtime",
		Sandbox: "runsc",
		RootFS:  rootfs,
		Cwd:     "/workspace",
	}
}
