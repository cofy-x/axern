package agent

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/lib/go/agentprofile"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEnsureServiceCreatesLabeledAgentService(t *testing.T) {
	client := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{},
		createResp:       &servicev1.CreateServiceResponse{Service: &servicev1.Service{ID: "svc-new"}},
	}
	envClient := &fakeEnvironmentClient{createResp: &environmentv1.CreateEnvironmentResponse{Environment: &environmentv1.Environment{ID: "env-new"}}}
	catalogClient := &fakeCatalogClient{template: fakeTemplate()}
	upstream, _ := url.Parse("https://api.example.test/v1")

	result, err := ensureService(context.Background(), Params{
		Workspace: "project-a",
		Profile: agentprofile.Profile{
			Name:         "deepseek-codex",
			Agent:        agentprofile.AgentCodex,
			ProviderType: agentprofile.ProviderOpenAI,
			Upstream:     upstream,
			Token:        "sk-test",
			Namespace:    "dev",
		},
		ServiceClient: client,
		Catalog:       catalogClient,
		Environment:   envClient,
	}, codexAdapter{}, testBundleRuntime())
	if err != nil {
		t.Fatalf("ensureService returned error: %v", err)
	}
	if result.ServiceID != "svc-new" || !result.Created {
		t.Fatalf("ensureService = %+v, want created svc-new", result)
	}
	labels := client.createReq.GetLabels()
	if labels[LabelWorkflow] != "agent" || labels[LabelWorkspace] != "project-a" || labels[LabelAgent] != "codex" || labels[LabelProfile] != "deepseek-codex" {
		t.Fatalf("labels = %#v", labels)
	}
	if client.createReq.GetReplicas() != 1 {
		t.Fatalf("replicas = %d, want 1", client.createReq.GetReplicas())
	}
	mounts := client.createReq.GetConfig().GetVolumeMounts()
	if len(mounts) != 1 || mounts[0].GetName() != "agent-workspace-project-a" || mounts[0].GetTarget() != "/home/axern/workspace" || mounts[0].GetReadonly() || !proto.Equal(mounts[0], workspaceExecutionConfig("project-a", testBundleRuntime()).GetVolumeMounts()[0]) {
		t.Fatalf("workspace mounts = %#v", mounts)
	}
	if envClient.createReq.GetSpec().GetTemplateID() != "coding-base" {
		t.Fatalf("template = %q, want coding-base", envClient.createReq.GetSpec().GetTemplateID())
	}
}

func TestEnsureServiceReusesMatchingEnvironmentAndService(t *testing.T) {
	template := fakeTemplate()
	envClient := &fakeEnvironmentClient{listResp: &environmentv1.ListEnvironmentsResponse{Environments: []*environmentv1.Environment{
		{
			ID:     "env-deleted",
			Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_DELETED,
			Spec: &environmentv1.EnvironmentSpec{
				Namespace:       "dev",
				TemplateID:      template.GetID(),
				TemplateVersion: template.GetVersion(),
			},
			ResolvedTemplate: template,
		},
		{
			ID:     "env-existing",
			Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY,
			Spec: &environmentv1.EnvironmentSpec{
				Namespace:       "dev",
				TemplateID:      template.GetID(),
				TemplateVersion: template.GetVersion(),
			},
			ResolvedTemplate: template,
			CreatedAt:        timestamppb.New(time.Unix(1, 0)),
		},
		{
			ID:     "env-newer",
			Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY,
			Spec: &environmentv1.EnvironmentSpec{
				Namespace:       "dev",
				TemplateID:      template.GetID(),
				TemplateVersion: template.GetVersion(),
			},
			ResolvedTemplate: template,
			CreatedAt:        timestamppb.New(time.Unix(2, 0)),
		},
	}}}
	service := &servicev1.Service{
		ID:            "svc-existing",
		Namespace:     "dev",
		EnvironmentID: "env-existing",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Replicas:      1,
		ReadyReplicas: 1,
		Config:        workspaceExecutionConfig("project-a", testBundleRuntime()),
		Labels:        workspaceLabels("project-a", "deepseek-codex", "codex"),
	}
	serviceClient := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{service}},
		getResp:          &servicev1.GetServiceResponse{Service: service},
	}
	upstream, _ := url.Parse("https://api.example.test/v1")

	result, err := ensureService(context.Background(), Params{
		Workspace: "project-a",
		Profile: agentprofile.Profile{
			Name:         "deepseek-codex",
			Agent:        agentprofile.AgentCodex,
			ProviderType: agentprofile.ProviderOpenAI,
			Upstream:     upstream,
			Token:        "sk-test",
			Namespace:    "dev",
		},
		ServiceClient: serviceClient,
		Catalog:       &fakeCatalogClient{template: template},
		Environment:   envClient,
	}, codexAdapter{}, testBundleRuntime())
	if err != nil {
		t.Fatalf("ensureService returned error: %v", err)
	}
	if result.ServiceID != "svc-existing" || result.Created {
		t.Fatalf("ensureService = %+v, want reused svc-existing", result)
	}
	if envClient.createReq != nil || serviceClient.createReq != nil || len(serviceClient.updateReqs) != 0 {
		t.Fatal("matching agent environment and service must be reused")
	}
}

func TestEnsureServiceResumesAndSwitchesProfile(t *testing.T) {
	template := fakeTemplate()
	environment := func(id string, createdSeconds int64) *environmentv1.Environment {
		return &environmentv1.Environment{
			ID: id, Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY,
			Spec: &environmentv1.EnvironmentSpec{
				Namespace: "dev", TemplateID: template.GetID(), TemplateVersion: template.GetVersion(),
			},
			ResolvedTemplate: template,
			CreatedAt:        timestamppb.New(time.Unix(createdSeconds, 0)),
		}
	}
	envClient := &fakeEnvironmentClient{listResp: &environmentv1.ListEnvironmentsResponse{Environments: []*environmentv1.Environment{
		environment("env-newer", 2), environment("env-older", 1),
	}}}
	service := &servicev1.Service{ID: "svc-existing", Namespace: "dev", EnvironmentID: "env-older", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Replicas: 0, Version: 8, Config: workspaceExecutionConfig("project-a", testBundleRuntime()), Labels: workspaceLabels("project-a", "old-profile", "claude-code")}
	updated := &servicev1.Service{ID: "svc-existing", EnvironmentID: "env-newer", Replicas: 1, Version: 9,
		Config: workspaceExecutionConfig("project-a", testBundleRuntime()), Labels: workspaceLabels("project-a", "deepseek-codex", "codex")}
	serviceClient := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{service}},
		getResp:          &servicev1.GetServiceResponse{Service: service},
		updateResp:       &servicev1.UpdateServiceResponse{Service: updated},
	}
	upstream, _ := url.Parse("https://api.example.test/v1")

	result, err := ensureService(context.Background(), Params{
		Workspace: "project-a",
		Profile: agentprofile.Profile{
			Name: "deepseek-codex", Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
			Upstream: upstream, Token: "sk-test", Namespace: "other-profile-namespace",
		},
		ServiceClient: serviceClient,
		Catalog:       &fakeCatalogClient{template: template},
		Environment:   envClient,
	}, codexAdapter{}, testBundleRuntime())
	if err != nil {
		t.Fatalf("ensureService returned error: %v", err)
	}
	if result.ServiceID != "svc-existing" || result.Created || serviceClient.createReq != nil || len(serviceClient.updateReqs) != 1 {
		t.Fatalf("ensureService = %+v, updates=%d", result, len(serviceClient.updateReqs))
	}
	req := serviceClient.updateReqs[0]
	if req.GetExpectedVersion() != 8 || req.GetReplicas() != 1 || req.GetEnvironmentID() != "env-newer" || req.GetLabels()[LabelProfile] != "deepseek-codex" {
		t.Fatalf("workspace update = %#v", req)
	}
	if envClient.listReq.GetFilter().GetNamespace() != "dev" || serviceClient.listServicesReq.GetFilter().GetNamespace() != "" {
		t.Fatalf("workspace lookup namespace=%q environment namespace=%q", serviceClient.listServicesReq.GetFilter().GetNamespace(), envClient.listReq.GetFilter().GetNamespace())
	}
}

func TestEnsureServiceRejectsProfileSwitchWhileRunning(t *testing.T) {
	template := fakeTemplate()
	service := &servicev1.Service{ID: "svc-existing", Namespace: "dev", EnvironmentID: "env-ready", Replicas: 1, ReadyReplicas: 1, Version: 3, Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Config: workspaceExecutionConfig("project-a", testBundleRuntime()), Labels: workspaceLabels("project-a", "old-profile", "codex")}
	client := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{service}},
		getResp:          &servicev1.GetServiceResponse{Service: service},
	}
	envClient := &fakeEnvironmentClient{
		listResp:   &environmentv1.ListEnvironmentsResponse{},
		createResp: &environmentv1.CreateEnvironmentResponse{Environment: &environmentv1.Environment{ID: "env-must-not-be-created"}},
	}
	upstream, _ := url.Parse("https://api.example.test/v1")
	_, err := ensureService(context.Background(), Params{
		Workspace: "project-a",
		Profile: agentprofile.Profile{Name: "new-profile", Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
			Upstream: upstream, Token: "sk-test", Namespace: "dev"},
		ServiceClient: client, Catalog: &fakeCatalogClient{template: template}, Environment: envClient,
	}, codexAdapter{}, testBundleRuntime())
	if err == nil || !strings.Contains(err.Error(), "stop it before switching") {
		t.Fatalf("ensureService() error = %v", err)
	}
	if envClient.listReq != nil || envClient.createReq != nil {
		t.Fatalf("running profile switch performed environment operations: list=%#v create=%#v", envClient.listReq, envClient.createReq)
	}
}

func TestEnsureServiceRetriesOneVersionConflict(t *testing.T) {
	template := fakeTemplate()
	environment := &environmentv1.Environment{ID: "env-ready", Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY,
		Spec: &environmentv1.EnvironmentSpec{Namespace: "dev", TemplateID: template.GetID(), TemplateVersion: template.GetVersion()}, ResolvedTemplate: template}
	serviceV1 := &servicev1.Service{ID: "svc-existing", Namespace: "dev", EnvironmentID: "env-ready", Replicas: 0, Version: 3, Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Config: workspaceExecutionConfig("project-a", testBundleRuntime()), Labels: workspaceLabels("project-a", "profile-a", "codex")}
	serviceV2 := proto.Clone(serviceV1).(*servicev1.Service)
	serviceV2.Version = 4
	client := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{serviceV1}},
		getResponses:     []*servicev1.GetServiceResponse{{Service: serviceV1}, {Service: serviceV2}},
		updateErrs:       []error{status.Error(codes.Aborted, "version mismatch")},
		updateResponses:  []*servicev1.UpdateServiceResponse{nil, {Service: &servicev1.Service{ID: "svc-existing", Replicas: 1, Version: 5}}},
	}
	upstream, _ := url.Parse("https://api.example.test/v1")
	result, err := ensureService(context.Background(), Params{
		Workspace: "project-a",
		Profile: agentprofile.Profile{Name: "profile-a", Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
			Upstream: upstream, Token: "sk-test", Namespace: "dev"},
		ServiceClient: client, Catalog: &fakeCatalogClient{template: template},
		Environment: &fakeEnvironmentClient{listResp: &environmentv1.ListEnvironmentsResponse{Environments: []*environmentv1.Environment{environment}}},
	}, codexAdapter{}, testBundleRuntime())
	if err != nil || result.ServiceID != "svc-existing" || len(client.updateReqs) != 2 || client.updateReqs[1].GetExpectedVersion() != 4 {
		t.Fatalf("ensureService() = %+v err=%v requests=%#v", result, err, client.updateReqs)
	}
}

func TestResolveWorkspaceName(t *testing.T) {
	if got, err := ResolveWorkspaceName("", "profile-a"); err != nil || got != "profile-a" {
		t.Fatalf("ResolveWorkspaceName default = %q, %v", got, err)
	}
	for _, invalid := range []string{"Upper", "-leading", "contains space", strings.Repeat("a", WorkspaceNameMaxLength+1)} {
		if _, err := ResolveWorkspaceName(invalid, "profile-a"); err == nil {
			t.Fatalf("ResolveWorkspaceName(%q) error = nil", invalid)
		}
	}
}

func TestAdapterCommandsQuoteArguments(t *testing.T) {
	command, requestTTY := codexAdapter{}.RunCommand("/opt/axern/agents/codex cli", []string{"exec", "hello world", "it's ok"})
	want := "'/opt/axern/agents/codex cli' exec 'hello world' 'it'\"'\"'s ok'"
	if command != want || requestTTY {
		t.Fatalf("codex command = %q tty=%t, want %q false", command, requestTTY, want)
	}
	command, requestTTY = claudeCodeAdapter{}.RunCommand("claude", nil)
	if command != "claude" || !requestTTY {
		t.Fatalf("claude command = %q tty=%t, want claude true", command, requestTTY)
	}
}

func TestInteractiveShellAddsBundlePathAfterLoginInitialization(t *testing.T) {
	command := commandInWorkspaceWithEnv(
		map[string]string{"AXERN_TEST_TOKEN": "secret value"},
		interactiveShellWithBundlePath(testBundleRuntime()),
	)
	want := `cd "/home/axern/workspace" && env 'AXERN_TEST_TOKEN=secret value' /bin/bash -lc 'export PATH=/opt/axern/agents/codex/bin:"$PATH"; exec /bin/bash -i'`
	if command != want {
		t.Fatalf("shell command = %q, want %q", command, want)
	}
}

func TestCommandWithEnvPrefixesSortedRemoteEnv(t *testing.T) {
	command := commandWithEnv(map[string]string{
		"Z_FLAG": "last value",
		"A_FLAG": "first",
	}, "codex exec hello")
	want := "env A_FLAG=first 'Z_FLAG=last value' codex exec hello"
	if command != want {
		t.Fatalf("commandWithEnv = %q, want %q", command, want)
	}
}

func TestCommandInWorkspaceWithEnvChangesDirectoryFirst(t *testing.T) {
	command := commandInWorkspaceWithEnv(map[string]string{"OPENAI_API_KEY": "local-token"}, "codex exec hello")
	for _, expected := range []string{
		`cd "/home/axern/workspace" && `,
		"OPENAI_API_KEY=local-token",
		"codex exec hello",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("commandInWorkspaceWithEnv() = %q, want %q", command, expected)
		}
	}
}

func TestCodexSessionEnvInjectsProxyCredentials(t *testing.T) {
	env := mergeCommandEnv(
		map[string]string{"OPENAI_API_KEY": "stale-token", "CUSTOM_FLAG": "1"},
		codexAdapter{}.SessionEnv(35748, agentprofile.Profile{}, "local-token"),
	)
	command := commandWithEnv(env, "/bin/bash -l")
	for _, expected := range []string{
		"OPENAI_API_KEY=local-token",
		"OPENAI_BASE_URL=http://127.0.0.1:35748",
		"CUSTOM_FLAG=1",
		"/bin/bash -l",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("commandWithEnv() = %q, want %q", command, expected)
		}
	}
	if strings.Contains(command, "stale-token") {
		t.Fatalf("commandWithEnv() = %q, must not keep stale profile token", command)
	}
}

func TestConfigBoolParsesUpstreamNoProxy(t *testing.T) {
	if !configBool(map[string]string{"upstream_no_proxy": "true"}, "upstream_no_proxy") {
		t.Fatal("configBool should accept true")
	}
	if configBool(map[string]string{"upstream_no_proxy": "not-bool"}, "upstream_no_proxy") {
		t.Fatal("configBool should reject invalid booleans")
	}
}

func TestValidateProfileRejectsWrongProvider(t *testing.T) {
	upstream, _ := url.Parse("https://api.example.test/v1")
	err := ValidateProfile(agentprofile.Profile{
		Name:         "bad",
		Agent:        agentprofile.AgentCodex,
		ProviderType: agentprofile.ProviderAnthropic,
		Upstream:     upstream,
		Token:        "sk-test",
	})
	if err == nil {
		t.Fatal("ValidateProfile error = nil")
	}
}

func TestDoctorReportsProviderFailureAndPlatformState(t *testing.T) {
	upstream, _ := url.Parse("https://api.example.test/v1")
	template := fakeTemplate()
	prober := &fakeProviderProber{
		result: agentprofile.ProbeResult{WireAPI: "responses", Endpoint: "https://api.example.test/v1/responses", ErrorClass: agentprofile.ProbeErrorUnsupportedProtocol, Message: "unsupported"},
		err:    fmt.Errorf("unsupported"),
	}
	result, err := (Control{}).Doctor(context.Background(), DoctorParams{
		Workspace: "project-a",
		Profile: agentprofile.Profile{
			Name: "deepseek-codex", Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
			WireAPI: agentprofile.WireAPIResponses, Upstream: upstream, Token: "sk-test",
		},
		ProbeModel: "deepseek-chat", ProviderProber: prober,
		Catalog: &fakeCatalogClient{template: template},
		Environment: &fakeEnvironmentClient{listResp: &environmentv1.ListEnvironmentsResponse{Environments: []*environmentv1.Environment{{
			ID: "env-ready", Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY,
			Spec:             &environmentv1.EnvironmentSpec{Namespace: "default", TemplateID: template.GetID(), TemplateVersion: template.GetVersion()},
			ResolvedTemplate: template,
		}}}},
		ServiceClient: &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{{
			ID: "svc-ready", EnvironmentID: "env-ready", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY, Replicas: 1, ReadyReplicas: 1,
			Config: workspaceExecutionConfig("project-a", testBundleRuntime()), Labels: workspaceLabels("project-a", "deepseek-codex", "codex"),
		}}}},
	})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if result.UpstreamCheck == nil || result.UpstreamCheck.ErrorClass != agentprofile.ProbeErrorUnsupportedProtocol || result.ServiceID != "svc-ready" || result.Workspace != "project-a" || result.WorkspaceTemplate != "coding-base" || result.AgentBundle != "codex" || result.LifecycleState != LifecycleRunning || !result.Persistent || result.Recommendation != "unsupported; agent workspace is ready" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDoctorMarksMissingProbeModelAsInvalidConfig(t *testing.T) {
	upstream, _ := url.Parse("https://api.example.test/v1")
	prober := &fakeProviderProber{
		result: agentprofile.ProbeResult{WireAPI: "responses", ErrorClass: agentprofile.ProbeErrorInvalidConfig, Message: "provider capability probe requires a model"},
		err:    fmt.Errorf("provider capability probe requires a model"),
	}
	result, err := (Control{}).Doctor(context.Background(), DoctorParams{
		Profile: agentprofile.Profile{
			Name: "codex", Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
			WireAPI: agentprofile.WireAPIResponses, Upstream: upstream, Token: "sk-test",
		},
		ProviderProber: prober,
		Catalog:        &fakeCatalogClient{template: fakeTemplate()},
		Environment:    &fakeEnvironmentClient{},
		ServiceClient:  &fakeServiceClient{},
	})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if result.ConfigOK || result.UpstreamCheck == nil || result.UpstreamCheck.ErrorClass != agentprofile.ProbeErrorInvalidConfig {
		t.Fatalf("result = %+v", result)
	}
}

func TestDoctorReportsRunningWorkspaceRuntimeDrift(t *testing.T) {
	upstream, _ := url.Parse("https://api.example.test/v1")
	template := fakeTemplate()
	envClient := &fakeEnvironmentClient{listResp: &environmentv1.ListEnvironmentsResponse{}}
	result, err := (Control{}).Doctor(context.Background(), DoctorParams{
		Workspace: "project-a",
		Profile: agentprofile.Profile{
			Name: "codex", Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
			WireAPI: agentprofile.WireAPIResponses, Upstream: upstream, Token: "sk-test",
		},
		ProbeModel:     "model",
		ProviderProber: &fakeProviderProber{result: agentprofile.ProbeResult{Compatible: true}},
		Catalog:        &fakeCatalogClient{template: template},
		Environment:    envClient,
		ServiceClient: &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{{
			ID: "svc-ready", EnvironmentID: "env-stale", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
			Replicas: 1, ReadyReplicas: 1, Config: workspaceExecutionConfig("project-a", testBundleRuntime()),
			Labels: workspaceLabels("project-a", "codex", "codex"),
		}}}},
	})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if result.Recommendation != "workspace base template does not match the catalog; stop the workspace before updating it" || envClient.createReq != nil {
		t.Fatalf("result = %+v environment create = %#v", result, envClient.createReq)
	}
}

func TestDoctorReportsProviderAndPlatformFailuresTogether(t *testing.T) {
	upstream, _ := url.Parse("https://api.example.test/v1")
	result, err := (Control{}).Doctor(context.Background(), DoctorParams{
		Profile: agentprofile.Profile{
			Name: "codex", Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
			WireAPI: agentprofile.WireAPIResponses, Upstream: upstream, Token: "sk-test",
		},
		ProbeModel: "model",
		ProviderProber: &fakeProviderProber{
			result: agentprofile.ProbeResult{ErrorClass: agentprofile.ProbeErrorUnsupportedProtocol, Message: "unsupported"},
			err:    fmt.Errorf("unsupported"),
		},
		Catalog:       &fakeCatalogClient{err: status.Error(codes.Unavailable, "offline")},
		Environment:   &fakeEnvironmentClient{},
		ServiceClient: &fakeServiceClient{},
	})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if result.UpstreamCheck == nil || result.UpstreamCheck.Compatible || result.PlatformCheck == nil || result.PlatformCheck.Reachable || result.PlatformCheck.ErrorClass != "unavailable" {
		t.Fatalf("result = %+v", result)
	}
	if result.Recommendation != "unsupported; resolve agent bundle failed (unavailable): rpc error: code = Unavailable desc = offline" {
		t.Fatalf("recommendation = %q", result.Recommendation)
	}
}

func TestDoctorReportsInvalidBundleMount(t *testing.T) {
	upstream, _ := url.Parse("https://api.example.test/v1")
	template := fakeTemplate()
	config := workspaceExecutionConfig("project-a", testBundleRuntime())
	config.ImageMounts[0].Readonly = false
	result, err := (Control{}).Doctor(context.Background(), DoctorParams{
		Workspace: "project-a",
		Profile: agentprofile.Profile{
			Name: "codex", Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
			WireAPI: agentprofile.WireAPIResponses, Upstream: upstream, Token: "sk-test",
		},
		ProbeModel:     "model",
		ProviderProber: &fakeProviderProber{result: agentprofile.ProbeResult{Compatible: true}},
		Catalog:        &fakeCatalogClient{template: template},
		Environment: &fakeEnvironmentClient{listResp: &environmentv1.ListEnvironmentsResponse{Environments: []*environmentv1.Environment{{
			ID: "env-ready", Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY,
			Spec: &environmentv1.EnvironmentSpec{Namespace: "default", TemplateID: template.GetID(), TemplateVersion: template.GetVersion()}, ResolvedTemplate: template,
		}}}},
		ServiceClient: &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{{
			ID: "svc-ready", EnvironmentID: "env-ready", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
			Replicas: 1, ReadyReplicas: 1, Config: config, Labels: workspaceLabels("project-a", "codex", "codex"),
		}}}},
	})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if result.Persistent || !strings.Contains(result.Recommendation, "read-only agent bundle image mount") {
		t.Fatalf("result = %+v", result)
	}
}

func TestResolveAgentBundleRejectsInvalidCatalogEntries(t *testing.T) {
	for _, tt := range []struct {
		name    string
		bundle  *catalogv1.AgentBundle
		wantErr string
	}{
		{name: "missing bundle", wantErr: "was not returned"},
		{name: "missing image", bundle: &catalogv1.AgentBundle{ID: "codex", BinaryPath: "/bin/codex"}, wantErr: "image ref is missing"},
		{name: "unsafe binary path", bundle: &catalogv1.AgentBundle{ID: "codex", BinaryPath: "/bin/../codex", ImageDescriptor: &catalogv1.OciImageDescriptor{Annotations: map[string]string{imageRefAnnotationKey: "example/codex:1"}}}, wantErr: "binary path"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveAgentBundle(context.Background(), &fakeCatalogClient{bundle: tt.bundle}, codexAdapter{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("resolveAgentBundle() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

type fakeProviderProber struct {
	result agentprofile.ProbeResult
	err    error
	model  string
}

func (f *fakeProviderProber) Probe(_ context.Context, request agentprofile.ProbeRequest) (agentprofile.ProbeResult, error) {
	f.model = request.Model
	return f.result, f.err
}

type fakeServiceClient struct {
	listServicesReq       *servicev1.ListServicesRequest
	listServicesResp      *servicev1.ListServicesResponse
	listServicesResponses []*servicev1.ListServicesResponse
	listServicesCalls     int
	createReq             *servicev1.CreateServiceRequest
	createResp            *servicev1.CreateServiceResponse
	getResp               *servicev1.GetServiceResponse
	getResponses          []*servicev1.GetServiceResponse
	getCalls              int
	updateReqs            []*servicev1.UpdateServiceRequest
	updateResp            *servicev1.UpdateServiceResponse
	updateResponses       []*servicev1.UpdateServiceResponse
	updateErrs            []error
	listReplicasResp      *servicev1.ListServiceReplicasResponse
	deleteReq             *servicev1.DeleteServiceRequest
	deleteResp            *servicev1.DeleteServiceResponse
	deleteErr             error
	deleteReqs            []*servicev1.DeleteServiceRequest
	deleteResponses       []*servicev1.DeleteServiceResponse
	deleteErrs            []error
}

func (f *fakeServiceClient) CreateService(ctx context.Context, req *servicev1.CreateServiceRequest, _ ...grpc.CallOption) (*servicev1.CreateServiceResponse, error) {
	f.createReq = req
	return f.createResp, nil
}
func (f *fakeServiceClient) GetService(context.Context, *servicev1.GetServiceRequest, ...grpc.CallOption) (*servicev1.GetServiceResponse, error) {
	if f.getCalls < len(f.getResponses) {
		response := f.getResponses[f.getCalls]
		f.getCalls++
		return response, nil
	}
	f.getCalls++
	return f.getResp, nil
}
func (f *fakeServiceClient) ListServices(ctx context.Context, req *servicev1.ListServicesRequest, _ ...grpc.CallOption) (*servicev1.ListServicesResponse, error) {
	f.listServicesReq = req
	index := f.listServicesCalls
	f.listServicesCalls++
	if index < len(f.listServicesResponses) {
		response := f.listServicesResponses[index]
		return response, nil
	}
	return f.listServicesResp, nil
}
func (f *fakeServiceClient) UpdateService(_ context.Context, req *servicev1.UpdateServiceRequest, _ ...grpc.CallOption) (*servicev1.UpdateServiceResponse, error) {
	f.updateReqs = append(f.updateReqs, req)
	index := len(f.updateReqs) - 1
	if index < len(f.updateErrs) && f.updateErrs[index] != nil {
		return nil, f.updateErrs[index]
	}
	if index < len(f.updateResponses) {
		return f.updateResponses[index], nil
	}
	return f.updateResp, nil
}
func (f *fakeServiceClient) DeleteService(_ context.Context, req *servicev1.DeleteServiceRequest, _ ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error) {
	f.deleteReq = req
	f.deleteReqs = append(f.deleteReqs, req)
	index := len(f.deleteReqs) - 1
	if index < len(f.deleteErrs) && f.deleteErrs[index] != nil {
		return nil, f.deleteErrs[index]
	}
	if index < len(f.deleteResponses) {
		return f.deleteResponses[index], nil
	}
	if f.deleteResp != nil || f.deleteErr != nil {
		return f.deleteResp, f.deleteErr
	}
	return nil, fmt.Errorf("agent workspace lifecycle must not delete services")
}
func (f *fakeServiceClient) ListServiceReplicas(context.Context, *servicev1.ListServiceReplicasRequest, ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error) {
	return f.listReplicasResp, nil
}
func (f *fakeServiceClient) ListServiceEvents(context.Context, *servicev1.ListServiceEventsRequest, ...grpc.CallOption) (*servicev1.ListServiceEventsResponse, error) {
	return nil, nil
}

type fakeEnvironmentClient struct {
	createReq  *environmentv1.CreateEnvironmentRequest
	createResp *environmentv1.CreateEnvironmentResponse
	listReq    *environmentv1.ListEnvironmentsRequest
	listResp   *environmentv1.ListEnvironmentsResponse
}

func (f *fakeEnvironmentClient) CreateEnvironment(ctx context.Context, req *environmentv1.CreateEnvironmentRequest, _ ...grpc.CallOption) (*environmentv1.CreateEnvironmentResponse, error) {
	f.createReq = req
	return f.createResp, nil
}
func (f *fakeEnvironmentClient) GetEnvironment(context.Context, *environmentv1.GetEnvironmentRequest, ...grpc.CallOption) (*environmentv1.GetEnvironmentResponse, error) {
	return nil, nil
}
func (f *fakeEnvironmentClient) ListEnvironments(_ context.Context, req *environmentv1.ListEnvironmentsRequest, _ ...grpc.CallOption) (*environmentv1.ListEnvironmentsResponse, error) {
	f.listReq = req
	return f.listResp, nil
}

type fakeCatalogClient struct {
	template *catalogv1.RuntimeTemplate
	bundle   *catalogv1.AgentBundle
	err      error
}

func (f *fakeCatalogClient) ListRuntimeTemplates(context.Context, *catalogv1.ListRuntimeTemplatesRequest, ...grpc.CallOption) (*catalogv1.ListRuntimeTemplatesResponse, error) {
	return nil, nil
}
func (f *fakeCatalogClient) GetRuntimeTemplate(context.Context, *catalogv1.GetRuntimeTemplateRequest, ...grpc.CallOption) (*catalogv1.GetRuntimeTemplateResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &catalogv1.GetRuntimeTemplateResponse{RuntimeTemplate: f.template}, nil
}
func (f *fakeCatalogClient) ListAgentBundles(context.Context, *catalogv1.ListAgentBundlesRequest, ...grpc.CallOption) (*catalogv1.ListAgentBundlesResponse, error) {
	return &catalogv1.ListAgentBundlesResponse{AgentBundles: []*catalogv1.AgentBundle{fakeAgentBundle()}}, nil
}
func (f *fakeCatalogClient) GetAgentBundle(context.Context, *catalogv1.GetAgentBundleRequest, ...grpc.CallOption) (*catalogv1.GetAgentBundleResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	bundle := f.bundle
	if bundle == nil && f.template != nil {
		bundle = fakeAgentBundle()
	}
	return &catalogv1.GetAgentBundleResponse{AgentBundle: bundle}, nil
}

func TestWorkspaceExecutionConfigUsesClaudeCodePrivateImageTarget(t *testing.T) {
	bundle := bundleRuntime{
		ID:          "claude-code",
		Image:       "axern/claude-code-bundle:dev",
		MountTarget: "/opt/axern/agents/claude-code",
		ImageTarget: "/__claude_code",
	}

	mounts := workspaceExecutionConfig("project-a", bundle).GetImageMounts()
	if len(mounts) != 1 || mounts[0].GetTarget() != "/__claude_code" || !mounts[0].GetReadonly() {
		t.Fatalf("image mounts = %#v", mounts)
	}
}

func fakeTemplate() *catalogv1.RuntimeTemplate {
	return &catalogv1.RuntimeTemplate{
		ID:      "coding-base",
		Version: "24.04.0",
	}
}

func fakeAgentBundle() *catalogv1.AgentBundle {
	return &catalogv1.AgentBundle{
		ID: "codex", Version: "0.144.6", BinaryPath: "/bin/codex",
		ImageDescriptor: &catalogv1.OciImageDescriptor{Annotations: map[string]string{imageRefAnnotationKey: "axern/codex-bundle:dev"}},
	}
}

func testBundleRuntime() bundleRuntime {
	return bundleRuntime{
		ID: "codex", Version: "0.144.6", Image: "axern/codex-bundle:dev",
		MountTarget: "/opt/axern/agents/codex", BinDir: "/opt/axern/agents/codex/bin", Binary: "/opt/axern/agents/codex/bin/codex",
	}
}
