package langruntime

import (
	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/protobuf/proto"
)

func (lr *LanguageRuntime) LoadOrPrepareBundleTemplate(
	prepare func() (*runtimeoci.BundleTemplate, error),
) (*runtimeoci.BundleTemplate, bool, error) {
	if lr == nil {
		return nil, false, nil
	}

	for {
		lr.templateMu.Lock()
		if lr.template != nil {
			template := lr.template
			lr.templateMu.Unlock()
			return template, true, nil
		}
		if lr.templateReady != nil {
			waitCh := lr.templateReady
			lr.templateMu.Unlock()
			<-waitCh
			continue
		}
		waitCh := make(chan struct{})
		lr.templateReady = waitCh
		lr.templateMu.Unlock()

		template, err := prepare()

		lr.templateMu.Lock()
		if err == nil {
			lr.template = template
		}
		lr.templateReady = nil
		close(waitCh)
		lr.templateMu.Unlock()
		return template, false, err
	}
}

func (lr *LanguageRuntime) ClearBundleTemplate() {
	if lr == nil {
		return
	}
	lr.templateMu.Lock()
	lr.template = nil
	lr.templateMu.Unlock()
}

func (lr *LanguageRuntime) RuntimeTemplate() *api.RuntimeTemplate {
	if lr == nil || lr.RootFS == nil {
		return nil
	}
	return &api.RuntimeTemplate{
		ID:               lr.ID,
		Sandbox:          lr.Sandbox,
		Rootfs:           rootfsConfigMessageFromRuntime(lr),
		Command:          append([]string(nil), lr.Command...),
		RuntimeEnvs:      cloneStringMap(lr.RuntimeEnvs),
		Cwd:              lr.Cwd,
		Mounts:           cloneMounts(lr.Mounts),
		ExecutionProfile: cloneRuntimeExecutionProfile(lr.ExecutionProfile),
	}
}

func (lr *LanguageRuntime) MatchesRuntimeTemplate(fr *api.RuntimeTemplate) bool {
	return languageRuntimeMatchesRuntimeTemplate(lr, fr)
}

func cloneRuntimeExecutionProfile(in *catalogv1.RuntimeExecutionProfile) *catalogv1.RuntimeExecutionProfile {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*catalogv1.RuntimeExecutionProfile)
}
