package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	demonginx "github.com/cofy-x/axern/runtime/axnoded/internal/demo/nginx"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type containerLister interface {
	List(context.Context, *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error)
}

type sandboxLifecycleService interface {
	Start(context.Context, *runtimeapi.StartRequest) (*runtimeapi.StartResponse, error)
	Delete(context.Context, *runtimeapi.DeleteRequest) (*runtimeapi.DeleteResponse, error)
	List(context.Context, *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error)
}

type NginxDashboard struct {
	svc        httpService
	natBackend string
}

func NewNginxDashboard(svc httpService, natBackend string) *NginxDashboard {
	return &NginxDashboard{svc: svc, natBackend: natBackend}
}

const dashboardActionTimeout = 90 * time.Second

type dashboardPageData struct {
	Ready      bool
	Message    string
	Error      string
	NATBackend string
	Instances  []managedInstanceView
}

type managedInstanceView struct {
	RuntimeName      string
	ManagedSandboxID string
	ContainerID      string
	State            string
	HostPort         int
	BrowserURL       string
	StdoutPath       string
	StderrPath       string
}

func (d *NginxDashboard) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.renderPage(w, r, "", "")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			d.redirectWithNotice(w, r, "", fmt.Sprintf("parse form: %v", err))
			return
		}
		runtimeName := r.FormValue("runtime")
		action := r.FormValue("action")
		ctx, cancel := context.WithTimeout(r.Context(), dashboardActionTimeout)
		defer cancel()

		var err error
		switch action {
		case "start":
			err = d.startManagedInstance(ctx, runtimeName)
		case "stop":
			err = d.stopManagedInstance(ctx, runtimeName)
		default:
			err = fmt.Errorf("unsupported action %q", action)
		}
		if err != nil {
			d.redirectWithNotice(w, r, "", err.Error())
			return
		}
		d.redirectWithNotice(w, r, fmt.Sprintf("%s %s ok", runtimeName, action), "")
	default:
		w.Header().Set("Allow", "GET")
		w.Header().Add("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (d *NginxDashboard) renderPage(w http.ResponseWriter, r *http.Request, message, renderErr string) {
	if message == "" {
		message = r.URL.Query().Get("message")
	}
	if renderErr == "" {
		renderErr = r.URL.Query().Get("error")
	}

	instances, err := d.listManagedInstances(r.Context())
	if err != nil {
		renderErr = err.Error()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := dashboardPageData{
		Ready:      d.svc.Ready(),
		Message:    message,
		Error:      renderErr,
		NATBackend: d.natBackend,
		Instances:  instances,
	}
	if err := dashboardPageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (d *NginxDashboard) redirectWithNotice(w http.ResponseWriter, r *http.Request, message, errText string) {
	values := url.Values{}
	if message != "" {
		values.Set("message", message)
	}
	if errText != "" {
		values.Set("error", errText)
	}
	target := "/demo/nginx"
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (d *NginxDashboard) listManagedInstances(ctx context.Context) ([]managedInstanceView, error) {
	views := make([]managedInstanceView, 0, 2)
	for _, runtimeName := range []string{config.RuntimeNameRunsc, config.RuntimeNameRunc} {
		spec, ok := demonginx.ManagedSpec(runtimeName)
		if !ok {
			continue
		}
		container, err := d.findManagedContainer(ctx, spec.SandboxID)
		if err != nil {
			return nil, err
		}
		view := managedInstanceView{
			RuntimeName:      runtimeName,
			ManagedSandboxID: spec.SandboxID,
			HostPort:         spec.HostPort,
			BrowserURL:       demonginx.BrowserURL(spec.HostPort),
			StdoutPath:       spec.StdoutPath,
			StderrPath:       spec.StderrPath,
			State:            "not running",
		}
		if container != nil {
			view.ContainerID = container.GetID()
			view.State = normalizeContainerState(container.GetState())
		}
		views = append(views, view)
	}
	return views, nil
}

func (d *NginxDashboard) startManagedInstance(ctx context.Context, runtimeName string) error {
	spec, ok := demonginx.ManagedSpec(runtimeName)
	if !ok {
		return fmt.Errorf("unsupported runtime %q", runtimeName)
	}
	if _, err := demonginx.WriteConfig(spec.ConfigDir); err != nil {
		return fmt.Errorf("write nginx config for %s: %w", runtimeName, err)
	}
	if err := d.deleteManagedSandbox(ctx, spec.SandboxID); err != nil {
		return err
	}

	startReq, err := resolvedSandboxStartRequest(spec.SandboxID, demonginx.BuildResolvedExecutionConfig(spec))
	if err != nil {
		return fmt.Errorf("build lifecycle request for %s: %w", runtimeName, err)
	}

	lifecycleSvc, err := d.lifecycleService()
	if err != nil {
		return err
	}
	resp, err := lifecycleSvc.Start(ctx, startReq)
	if err != nil {
		return fmt.Errorf("start %s nginx demo: %w", runtimeName, err)
	}
	if resp.GetCode() != 0 || resp.GetID() == "" {
		return fmt.Errorf("start %s nginx demo failed: code=%d message=%s id=%s", runtimeName, resp.GetCode(), resp.GetMessage(), resp.GetID())
	}
	return nil
}

func (d *NginxDashboard) stopManagedInstance(ctx context.Context, runtimeName string) error {
	spec, ok := demonginx.ManagedSpec(runtimeName)
	if !ok {
		return fmt.Errorf("unsupported runtime %q", runtimeName)
	}
	return d.deleteManagedSandbox(ctx, spec.SandboxID)
}

func (d *NginxDashboard) deleteManagedSandbox(ctx context.Context, sandboxID string) error {
	container, err := d.findManagedContainer(ctx, sandboxID)
	if err != nil {
		return err
	}
	if container == nil {
		return nil
	}
	lifecycleSvc, err := d.lifecycleService()
	if err != nil {
		return err
	}
	if _, err := lifecycleSvc.Delete(ctx, &runtimeapi.DeleteRequest{ID: sandboxID, Timeout: 0}); err != nil {
		return fmt.Errorf("delete sandbox %s: %w", sandboxID, err)
	}
	return nil
}

func (d *NginxDashboard) lifecycleService() (sandboxLifecycleService, error) {
	lifecycleSvc, ok := d.svc.(sandboxLifecycleService)
	if !ok {
		return nil, fmt.Errorf("dashboard sandbox lifecycle unavailable")
	}
	return lifecycleSvc, nil
}

func (d *NginxDashboard) findManagedContainer(ctx context.Context, sandboxID string) (*runtimeapi.ContainerStatus, error) {
	lister, ok := d.svc.(containerLister)
	if !ok {
		return nil, nil
	}
	listResp, err := lister.List(ctx, &runtimeapi.ListContainersRequest{ID: sandboxID})
	if err != nil {
		if grpcstatus.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("list containers for sandbox %s: %w", sandboxID, err)
	}
	if len(listResp.GetContainers()) == 0 {
		return nil, nil
	}
	return listResp.GetContainers()[0], nil
}

func normalizeContainerState(state runtimeapi.ContainerState) string {
	switch state {
	case runtimeapi.ContainerState_CONTAINER_RUNNING:
		return "running"
	case runtimeapi.ContainerState_CONTAINER_EXITED:
		return "exited"
	case runtimeapi.ContainerState_CONTAINER_UNKNOWN:
		return "unknown"
	default:
		return strings.ToLower(strings.TrimPrefix(state.String(), "CONTAINER_"))
	}
}
