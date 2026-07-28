package appfunction

import (
	"context"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

const (
	functionWorkerPort = 8080

	functionWorkerLabelOwner      = "axern.io/owner"
	functionWorkerLabelComponent  = "axern.io/component"
	functionWorkerLabelFunctionID = "axern.io/function-id"
	functionWorkerLabelRevisionID = "axern.io/function-revision-id"
	functionWorkerLabelName       = "axern.io/function-name"
)

type Controller struct {
	store         functionkernel.Store
	environments  EnvironmentControl
	services      WorkerServiceController
	serviceWatch  servicekernel.Watcher
	invoker       FunctionInvoker
	bundleBaseURL string
	bundleToken   string
}

type ControllerDeps struct {
	Store         functionkernel.Store
	Environments  EnvironmentControl
	Services      WorkerServiceController
	ServiceWatch  servicekernel.Watcher
	Invoker       FunctionInvoker
	BundleBaseURL string
	BundleToken   string
}

type EnvironmentControl interface {
	CreateEnvironment(ctx context.Context, spec *environmentv1.EnvironmentSpec, labels map[string]string, now time.Time) (*environmentv1.Environment, error)
	GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error)
}

type WorkerServiceController interface {
	Create(ctx context.Context, params servicekernel.CreateParams, now time.Time) (*servicev1.Service, error)
	Get(ctx context.Context, id string) (*servicev1.Service, bool, error)
	Update(ctx context.Context, req *servicev1.UpdateServiceRequest, now time.Time) (*servicev1.Service, error)
	Delete(ctx context.Context, params servicekernel.DeleteParams, now time.Time) (*servicev1.Service, bool, error)
}

func NewController(deps ControllerDeps) *Controller {
	return &Controller{
		store:         deps.Store,
		environments:  deps.Environments,
		services:      deps.Services,
		serviceWatch:  deps.ServiceWatch,
		invoker:       deps.Invoker,
		bundleBaseURL: strings.TrimSpace(deps.BundleBaseURL),
		bundleToken:   strings.TrimSpace(deps.BundleToken),
	}
}

var _ functionkernel.Control = (*Controller)(nil)
