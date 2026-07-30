package doctor

import (
	"context"
	"time"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc"
)

type Status string

const (
	StatusHealthy  Status = "healthy"
	StatusDegraded Status = "degraded"
	StatusFailed   Status = "failed"
)

type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
	CheckSkip CheckStatus = "skip"
)

type Check struct {
	Name        string      `json:"name"`
	Status      CheckStatus `json:"status"`
	Code        string      `json:"code"`
	DurationMS  int64       `json:"duration_ms"`
	Message     string      `json:"message"`
	Remediation string      `json:"remediation,omitempty"`
}

type Report struct {
	Status    Status  `json:"status"`
	Context   string  `json:"context,omitempty"`
	Namespace string  `json:"namespace"`
	Mode      string  `json:"mode"`
	Checks    []Check `json:"checks"`
}

func (r *Report) add(check Check) {
	r.Checks = append(r.Checks, check)
	switch check.Status {
	case CheckFail:
		r.Status = StatusFailed
	case CheckWarn:
		if r.Status == StatusHealthy {
			r.Status = StatusDegraded
		}
	}
}

type TLSConfig struct {
	CACert string
	Cert   string
	Key    string
}

type ProbeOptions struct {
	TemplateID   string
	RuntimeClass string
	Timeout      time.Duration
	CleanupWait  time.Duration
}

type Options struct {
	ContextName  string
	Namespace    string
	TLS          TLSConfig
	Probe        *ProbeOptions
	CheckTimeout time.Duration
	Open         SessionOpener
	Now          func() time.Time
}

type NamespaceClient interface {
	GetNamespace(context.Context, *namespacev1.GetNamespaceRequest, ...grpc.CallOption) (*namespacev1.GetNamespaceResponse, error)
}

type CatalogClient interface {
	ListRuntimeTemplates(context.Context, *catalogv1.ListRuntimeTemplatesRequest, ...grpc.CallOption) (*catalogv1.ListRuntimeTemplatesResponse, error)
}

type IdentityClient interface {
	WhoAmI(context.Context, *identityv1.WhoAmIRequest, ...grpc.CallOption) (*identityv1.WhoAmIResponse, error)
}

type EnvironmentClient interface {
	CreateEnvironment(context.Context, *environmentv1.CreateEnvironmentRequest, ...grpc.CallOption) (*environmentv1.CreateEnvironmentResponse, error)
	DeleteEnvironment(context.Context, *environmentv1.DeleteEnvironmentRequest, ...grpc.CallOption) (*environmentv1.DeleteEnvironmentResponse, error)
}

type RunClient interface {
	CreateRun(context.Context, *runv1.CreateRunRequest, ...grpc.CallOption) (*runv1.CreateRunResponse, error)
	GetRun(context.Context, *runv1.GetRunRequest, ...grpc.CallOption) (*runv1.GetRunResponse, error)
	ListRuns(context.Context, *runv1.ListRunsRequest, ...grpc.CallOption) (*runv1.ListRunsResponse, error)
	CancelRun(context.Context, *runv1.CancelRunRequest, ...grpc.CallOption) (*runv1.CancelRunResponse, error)
}

type Session struct {
	Context     context.Context
	Identity    IdentityClient
	Namespace   NamespaceClient
	Catalog     CatalogClient
	Environment EnvironmentClient
	Run         RunClient
	Close       func() error
}

type SessionOpener func(context.Context) (*Session, error)
