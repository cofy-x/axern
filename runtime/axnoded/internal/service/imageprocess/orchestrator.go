package imageprocess

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/google/uuid"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

const (
	KindLabel             = "axern.image_process.kind"
	ParentAllocationLabel = "axern.image_process.parent_allocation"
	ImageLabel            = "axern.image_process.image"
	Kind                  = "image_process"
	outputLimit           = 1 << 20
)

var idleCommand = []string{"/bin/sh", "-lc", "while true; do sleep 3600; done"}

type Target struct {
	Runtime string
	Spec    *specs.Spec
	Labels  map[string]string
}

type Options struct {
	InspectTarget       func(ctx context.Context, parentID string) (Target, contract.RuntimeHandler, error)
	EnsureRuntime       func(ctx context.Context, template *runtime.RuntimeTemplate) (*langrtmanager.LanguageRuntime, error)
	CreateContainer     func(ctx context.Context, lrt *langrtmanager.LanguageRuntime, templateRequest, createRequest *runtime.CreateContainerRequest) error
	LoadContainerLabels func(actorID string) (map[string]string, error)
	DeleteContainer     func(ctx context.Context, actorID string) error
	IgnoreDeleteError   func(error) bool
	Network             string
}

type Orchestrator struct {
	options Options
}

func NewOrchestrator(options Options) Orchestrator {
	return Orchestrator{options: options}
}

type Actor struct {
	ID      string
	Labels  map[string]string
	Handler contract.RuntimeHandler
	Cleanup func()
}

func (o Orchestrator) CreateActor(ctx context.Context, parentID string, spec *runtime.ImageProcessSpec) (*Actor, error) {
	target, targetHandler, err := o.options.InspectTarget(ctx, parentID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(target.Runtime) == "" {
		return nil, errord.ErrInvalidContainer
	}
	actorMounts, err := ResolveMounts(target.Spec, spec.GetMounts())
	if err != nil {
		return nil, err
	}

	lrtTemplate := RuntimeTemplate(target.Runtime, spec.GetImage())
	lrt, err := o.options.EnsureRuntime(ctx, lrtTemplate)
	if err != nil {
		return nil, fmt.Errorf("prepare image process runtime: %w", err)
	}
	lrt.IncRef()
	if err := EnsureMountTargets(lrt.RootFS.Path(), actorMounts); err != nil {
		lrt.DecRef()
		return nil, err
	}

	actorID := config.SandboxContainerPrefix + "-imageproc-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	labels := Labels(lrtTemplate.GetID(), parentID, spec.GetImage())
	createRequest := &runtime.CreateContainerRequest{
		Runtime:  target.Runtime,
		Command:  append([]string(nil), idleCommand...),
		Rootfs:   startplan.BuildContainerRootfs(lrt),
		Resource: startplan.ResourcesToLinux(nil),
		Mounts:   actorMounts,
		Envs:     startplan.BuildStaticRuntimeEnv(lrt),
		Network:  o.options.Network,
		Labels:   labels,
		Cwd:      lrt.Cwd,
		ID:       actorID,
	}
	templateRequest := &runtime.CreateContainerRequest{
		Runtime: target.Runtime,
		Command: append([]string(nil), idleCommand...),
		Rootfs:  startplan.BuildContainerRootfs(lrt),
		Mounts:  nil,
		Envs:    startplan.BuildStaticRuntimeEnv(lrt),
		Labels:  labels,
		Cwd:     lrt.Cwd,
	}

	if err := o.options.CreateContainer(ctx, lrt, templateRequest, createRequest); err != nil {
		lrt.DecRef()
		return nil, err
	}
	actorLabels, err := o.options.LoadContainerLabels(actorID)
	if err != nil {
		_ = o.options.DeleteContainer(context.Background(), actorID)
		lrt.DecRef()
		return nil, fmt.Errorf("load image process metadata: %w", errord.ErrFailedPrecondition)
	}

	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if deleteErr := o.options.DeleteContainer(cleanupCtx, actorID); deleteErr != nil && (o.options.IgnoreDeleteError == nil || !o.options.IgnoreDeleteError(deleteErr)) {
			logrus.WithError(deleteErr).Warnf("delete image process container %s failed", actorID)
		}
		lrt.DecRef()
	}
	return &Actor{
		ID:      actorID,
		Labels:  actorLabels,
		Handler: targetHandler,
		Cleanup: cleanup,
	}, nil
}
