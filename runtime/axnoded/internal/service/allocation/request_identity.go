package allocation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
)

// StartRequestDigest identifies the immutable behavioral contract of one
// managed allocation attempt. Placement evidence is deliberately reduced to
// key plus catalog loss policy: a later current observation may replace the
// selected placement proof without changing the requested sandbox. Trace IDs
// are request telemetry and likewise do not define runtime behavior.
func StartRequestDigest(request *runtimev1.StartRequest) (string, error) {
	if request == nil {
		return "", fmt.Errorf("allocation start request is required")
	}
	canonical := proto.Clone(request).(*runtimev1.StartRequest)
	canonical.TraceID = ""
	dependencies := make([]*capabilityv1.CapabilityDependency, 0, len(canonical.GetCapabilityDependencies()))
	for _, dependency := range canonical.GetCapabilityDependencies() {
		if dependency == nil {
			return "", fmt.Errorf("allocation capability dependency is required")
		}
		keyID, err := capabilitycontract.KeyID(dependency.GetKey())
		if err != nil {
			return "", err
		}
		policy, err := capabilitycontract.LossPolicy(dependency.GetKey())
		if err != nil {
			return "", err
		}
		if dependency.GetLossPolicy() != policy {
			return "", fmt.Errorf("allocation capability %q has invalid loss policy", keyID)
		}
		dependencies = append(dependencies, &capabilityv1.CapabilityDependency{
			Key:        capabilitycontract.CloneKey(dependency.GetKey()),
			LossPolicy: policy,
		})
	}
	sort.Slice(dependencies, func(i, j int) bool {
		left, _ := capabilitycontract.KeyID(dependencies[i].GetKey())
		right, _ := capabilitycontract.KeyID(dependencies[j].GetKey())
		return left < right
	})
	canonical.CapabilityDependencies = dependencies
	extensionRequirements := append([]*capabilityv1.ExtensionCapabilityRequirement(nil), canonical.GetExtensionCapabilityRequirements()...)
	for _, requirement := range extensionRequirements {
		if requirement == nil || requirement.GetCapability() == nil {
			return "", fmt.Errorf("extension capability requirement is required")
		}
	}
	sort.Slice(extensionRequirements, func(i, j int) bool {
		left := extensionRequirements[i].GetCapability()
		right := extensionRequirements[j].GetCapability()
		if left.GetName() != right.GetName() {
			return left.GetName() < right.GetName()
		}
		return left.GetValue() < right.GetValue()
	})
	canonical.ExtensionCapabilityRequirements = extensionRequirements
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical allocation start request: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validStartRequestDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil && len(decoded) == sha256.Size
}
