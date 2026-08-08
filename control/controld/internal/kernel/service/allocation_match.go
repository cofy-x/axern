package servicekernel

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/proto"
)

// DesiredSpecDigest identifies the immutable service execution intent copied
// into an allocation. Replica counts, rollout status, and other mutable service
// state are deliberately excluded.
func DesiredSpecDigest(service *servicev1.Service) (string, error) {
	if service == nil {
		return "", nil
	}
	hash := sha256.New()
	writePart := func(data []byte) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(data)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(data)
	}
	writePart([]byte(strings.TrimSpace(service.GetEnvironmentID())))
	for _, message := range []proto.Message{
		executionkernel.NormalizeConfig(service.GetConfig()),
		NormalizeReadinessProbe(service.GetReadinessProbe()),
		NormalizeLivenessProbe(service.GetLivenessProbe()),
	} {
		data, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
		if err != nil {
			return "", err
		}
		writePart(data)
	}
	return "v1:sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func allocationMatchesDesired(desiredSpecDigest string, service *servicev1.Service) bool {
	if service == nil || strings.TrimSpace(desiredSpecDigest) == "" {
		return false
	}
	desired, err := DesiredSpecDigest(service)
	return err == nil && desiredSpecDigest == desired
}

func AllocationMatchesDesired(desiredSpecDigest string, service *servicev1.Service) bool {
	return allocationMatchesDesired(desiredSpecDigest, service)
}

func allocationOutdated(desiredSpecDigest string, service *servicev1.Service) bool {
	return !allocationMatchesDesired(desiredSpecDigest, service)
}

func AllocationOutdated(desiredSpecDigest string, service *servicev1.Service) bool {
	return allocationOutdated(desiredSpecDigest, service)
}
