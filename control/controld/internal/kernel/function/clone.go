package functionkernel

import (
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func CloneFunction(in *functionv1.Function) *functionv1.Function {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.Function)
}

func CloneRevision(in *functionv1.FunctionRevision) *functionv1.FunctionRevision {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionRevision)
}

func CloneDeployment(in *functionv1.FunctionDeployment) *functionv1.FunctionDeployment {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionDeployment)
}

func CloneSpec(in *functionv1.FunctionSpec) *functionv1.FunctionSpec {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionSpec)
}

func CloneSource(in *functionv1.FunctionSource) *functionv1.FunctionSource {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionSource)
}

func CloneScaling(in *functionv1.FunctionScalingSpec) *functionv1.FunctionScalingSpec {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionScalingSpec)
}

func CloneInvocation(in *functionv1.FunctionInvocation) *functionv1.FunctionInvocation {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionInvocation)
}

func ClonePayload(in *functionv1.FunctionPayload) *functionv1.FunctionPayload {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionPayload)
}

func CloneResult(in *functionv1.FunctionResult) *functionv1.FunctionResult {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionResult)
}

func CloneError(in *functionv1.FunctionError) *functionv1.FunctionError {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*functionv1.FunctionError)
}

func cloneDuration(in *durationpb.Duration) *durationpb.Duration {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*durationpb.Duration)
}
