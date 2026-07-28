package agent

import (
	"fmt"
	"io"
	"strconv"

	appagent "github.com/cofy-x/axern/apps/cli/internal/application/agent"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

func renderRuntimeList(w io.Writer, runtimes []appagent.RuntimeSummary, format output.Format) error {
	if format == output.FormatJSON {
		return output.PrintJSON(w, runtimes)
	}
	rows := make([][]string, 0, len(runtimes))
	for _, runtime := range runtimes {
		rows = append(rows, []string{
			runtime.Workspace,
			runtime.LifecycleState,
			runtime.Profile,
			runtime.Agent,
			runtime.ServiceID,
			runtime.Namespace,
			strconv.Itoa(int(runtime.Ready)) + "/" + strconv.Itoa(int(runtime.Desired)),
		})
	}
	output.RenderTable(w, []string{"WORKSPACE", "STATE", "PROFILE", "AGENT", "SERVICE", "NAMESPACE", "READY"}, rows)
	return nil
}

func renderStopResult(w io.Writer, result appagent.StopResult, format output.Format) error {
	if format == output.FormatJSON {
		return output.PrintJSON(w, result)
	}
	fmt.Fprintf(w, "Agent workspace suspended: %s (service=%s)\n", result.Workspace, result.ServiceID)
	return nil
}

func renderDeleteResult(w io.Writer, result appagent.DeleteResult, format output.Format) error {
	if format == output.FormatJSON {
		return output.PrintJSON(w, result)
	}
	fmt.Fprintf(w, "Agent workspace deleted: %s (service=%s)\n", result.Workspace, result.ServiceID)
	return nil
}

type profileOutputJSON struct {
	Name          string            `json:"name"`
	Agent         string            `json:"agent"`
	Provider      string            `json:"provider"`
	WireAPI       string            `json:"wire_api"`
	Upstream      string            `json:"upstream,omitempty"`
	TokenSet      bool              `json:"token_set"`
	TemplateID    string            `json:"template_id,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	RemoteUser    string            `json:"remote_user,omitempty"`
	RestoreOnExit bool              `json:"restore_on_exit"`
	Env           map[string]string `json:"env,omitempty"`
	Config        map[string]string `json:"config,omitempty"`
}

type resultJSON struct {
	Agent          string `json:"agent"`
	Profile        string `json:"profile"`
	Workspace      string `json:"workspace"`
	ServiceID      string `json:"service_id"`
	CreatedService bool   `json:"created_service"`
	AllocationID   string `json:"allocation_id"`
	NodeID         string `json:"node_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	BoundAddr      string `json:"bound_addr,omitempty"`
	RelayID        string `json:"relay_id,omitempty"`
	ClientTarget   string `json:"client_edge_target,omitempty"`
	NodeTarget     string `json:"node_edge_target,omitempty"`
	Upstream       string `json:"upstream"`
}

func renderReady(w io.Writer, result appagent.Result, format output.Format) error {
	if format == output.FormatJSON {
		out := resultJSON{
			Agent:          result.Agent,
			Profile:        result.ProfileName,
			Workspace:      result.Workspace,
			ServiceID:      result.ServiceID,
			CreatedService: result.CreatedService,
			AllocationID:   result.AllocationID,
			NodeID:         result.NodeID,
			Upstream:       result.Upstream,
		}
		if result.Session != nil {
			out.SessionID = result.Session.GetSessionID()
			out.BoundAddr = result.Session.GetBoundAddr()
			out.RelayID = result.Session.GetRelayID()
			out.ClientTarget = result.Session.GetClientEdgeTarget()
			out.NodeTarget = result.Session.GetNodeEdgeTarget()
		}
		return output.PrintJSON(w, out)
	}
	fmt.Fprintf(w, "Agent: %s\n", result.Agent)
	fmt.Fprintf(w, "Agent profile: %s\n", result.ProfileName)
	fmt.Fprintf(w, "Agent workspace: %s\n", result.Workspace)
	if result.CreatedService {
		fmt.Fprintf(w, "Agent service created: %s\n", result.ServiceID)
	} else {
		fmt.Fprintf(w, "Agent service reused: %s\n", result.ServiceID)
	}
	fmt.Fprintf(w, "Remote runtime ready: allocation=%s", result.AllocationID)
	if result.NodeID != "" {
		fmt.Fprintf(w, " node=%s", result.NodeID)
	}
	fmt.Fprintln(w)
	if result.Session != nil {
		fmt.Fprintf(w, "Tunnel session: %s\n", result.Session.GetSessionID())
	}
	fmt.Fprintf(w, "Remote agent base URL: http://%s\n", result.RemoteBindAddress)
	fmt.Fprintf(w, "Upstream: %s\n", result.Upstream)
	return nil
}

func renderProfile(w io.Writer, name string, profile *agentprofile.ProfileConfig) {
	fmt.Fprintf(w, "Name: %s\n", name)
	fmt.Fprintf(w, "Agent: %s\n", profile.Agent)
	fmt.Fprintf(w, "Provider: %s\n", profile.Provider)
	fmt.Fprintf(w, "Wire API: %s\n", profile.WireAPI)
	fmt.Fprintf(w, "Upstream: %s\n", profile.Upstream)
	fmt.Fprintf(w, "Token: %s\n", configuredLabel(profile.Token != ""))
	fmt.Fprintf(w, "Template: %s\n", profile.TemplateID)
	fmt.Fprintf(w, "Namespace: %s\n", profile.Namespace)
	fmt.Fprintf(w, "Remote User: %s\n", profile.RemoteUser)
	if len(profile.Env) > 0 {
		fmt.Fprintf(w, "Env: %v\n", profile.Env)
	}
	if len(profile.Config) > 0 {
		fmt.Fprintf(w, "Config: %v\n", profile.Config)
	}
	fmt.Fprintf(w, "Restore On Exit: %t\n", profile.RestoreOnExit)
}

func profileJSON(name string, profile *agentprofile.ProfileConfig) profileOutputJSON {
	return profileOutputJSON{
		Name:          name,
		Agent:         profile.Agent,
		Provider:      profile.Provider,
		WireAPI:       profile.WireAPI,
		Upstream:      profile.Upstream,
		TokenSet:      profile.Token != "",
		TemplateID:    profile.TemplateID,
		Namespace:     profile.Namespace,
		RemoteUser:    profile.RemoteUser,
		RestoreOnExit: profile.RestoreOnExit,
		Env:           agentprofile.CopyMap(profile.Env),
		Config:        agentprofile.CopyMap(profile.Config),
	}
}

func configuredLabel(ok bool) string {
	if ok {
		return "configured"
	}
	return "missing"
}
