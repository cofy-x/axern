package environmentcache

import (
	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/protobuf/proto"
)

func (environment *PreparedEnvironment) LoadOrPrepareBundleTemplate(
	prepare func() (*runtimeoci.BundleTemplate, error),
) (*runtimeoci.BundleTemplate, bool, error) {
	if environment == nil {
		return nil, false, nil
	}

	for {
		environment.templateMu.Lock()
		if environment.template != nil {
			template := environment.template
			environment.templateMu.Unlock()
			return template, true, nil
		}
		if environment.templateReady != nil {
			waitCh := environment.templateReady
			environment.templateMu.Unlock()
			<-waitCh
			continue
		}
		waitCh := make(chan struct{})
		environment.templateReady = waitCh
		environment.templateMu.Unlock()

		template, err := prepare()

		environment.templateMu.Lock()
		if err == nil {
			environment.template = template
		}
		environment.templateReady = nil
		close(waitCh)
		environment.templateMu.Unlock()
		return template, false, err
	}
}

func (environment *PreparedEnvironment) ClearBundleTemplate() {
	if environment == nil {
		return
	}
	environment.templateMu.Lock()
	environment.template = nil
	environment.templateMu.Unlock()
}

func (environment *PreparedEnvironment) ResolvedEnvironment() *api.ResolvedEnvironment {
	if environment == nil || environment.RootFS == nil {
		return nil
	}
	return &api.ResolvedEnvironment{
		ID:               environment.ID,
		Rootfs:           rootfsConfigMessageFromRuntime(environment),
		Argv:             append([]string(nil), environment.Argv...),
		Env:              cloneStringMap(environment.Env),
		Cwd:              environment.Cwd,
		Mounts:           cloneMounts(environment.Mounts),
		ExecutionProfile: cloneOciExecutionProfile(environment.ExecutionProfile),
	}
}

func (environment *PreparedEnvironment) MatchesResolvedEnvironment(fr *api.ResolvedEnvironment) bool {
	return preparedEnvironmentMatchesTemplate(environment, fr)
}

func cloneOciExecutionProfile(in *environmentv1.OciExecutionProfile) *environmentv1.OciExecutionProfile {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*environmentv1.OciExecutionProfile)
}
