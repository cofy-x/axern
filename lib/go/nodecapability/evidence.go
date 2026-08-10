package nodecapability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	boundedIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+=,@-]{0,511}$`)
	digestPattern          = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	bootIDPattern          = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
)

func ConfigEvidence(configDigest string) *capabilityv1.CapabilityEvidence {
	evidence := &capabilityv1.CapabilityEvidence{Identity: &capabilityv1.CapabilityEvidence_Config{Config: &capabilityv1.ConfigEvidenceIdentity{ConfigDigest: configDigest}}}
	evidence.EvidenceID = EvidenceID(evidence)
	return evidence
}

func BootEvidence(bootID string) *capabilityv1.CapabilityEvidence {
	evidence := &capabilityv1.CapabilityEvidence{Identity: &capabilityv1.CapabilityEvidence_Boot{Boot: &capabilityv1.BootEvidenceIdentity{BootID: bootID}}}
	evidence.EvidenceID = EvidenceID(evidence)
	return evidence
}

func MountEvidence(bootID, mountIdentity string) *capabilityv1.CapabilityEvidence {
	evidence := &capabilityv1.CapabilityEvidence{Identity: &capabilityv1.CapabilityEvidence_Mount{Mount: &capabilityv1.MountEvidenceIdentity{BootID: bootID, MountIdentity: mountIdentity}}}
	evidence.EvidenceID = EvidenceID(evidence)
	return evidence
}

func RuntimeEvidence(bootID, runtimeName, binaryDigest, configDigest string) *capabilityv1.CapabilityEvidence {
	evidence := &capabilityv1.CapabilityEvidence{Identity: &capabilityv1.CapabilityEvidence_Runtime{Runtime: &capabilityv1.RuntimeEvidenceIdentity{BootID: bootID, RuntimeName: runtimeName, RuntimeBinaryDigest: binaryDigest, RuntimeConfigDigest: configDigest}}}
	evidence.EvidenceID = EvidenceID(evidence)
	return evidence
}

func DerivedEvidence(dependencies ...*capabilityv1.CapabilityObservationProof) *capabilityv1.CapabilityEvidence {
	evidence := &capabilityv1.CapabilityEvidence{Identity: &capabilityv1.CapabilityEvidence_Derived{Derived: &capabilityv1.DerivedEvidenceIdentity{
		CatalogDigest:            CatalogDigest(),
		DependencyEvidenceDigest: dependencyEvidenceDigest(dependencies),
	}}}
	evidence.EvidenceID = EvidenceID(evidence)
	return evidence
}

// dependencyEvidenceDigest binds a derived subject to the catalog dependency
// keys and their typed evidence identities. Observation IDs and timestamps are
// deliberately excluded so a normal refresh of the same subjects does not
// manufacture an identity transition.
func dependencyEvidenceDigest(dependencies []*capabilityv1.CapabilityObservationProof) string {
	items := make([]string, 0, len(dependencies))
	for _, proof := range dependencies {
		keyID, err := KeyID(proof.GetKey())
		if err != nil || proof.GetEvidence().GetEvidenceID() == "" {
			return ""
		}
		items = append(items, keyID+"\x00"+proof.GetEvidence().GetEvidenceID())
	}
	sort.Strings(items)
	hash := sha256.New()
	for _, item := range items {
		_, _ = hash.Write([]byte(item))
		_, _ = hash.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// EvidenceID is a canonical digest of the typed subject identity. It never
// includes observation time, state, or diagnostics.
func EvidenceID(evidence *capabilityv1.CapabilityEvidence) string {
	if evidence == nil || evidence.GetIdentity() == nil {
		return ""
	}
	clone := proto.Clone(evidence).(*capabilityv1.CapabilityEvidence)
	clone.EvidenceID = ""
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(clone)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func EvidenceIdentityKind(evidence *capabilityv1.CapabilityEvidence) IdentityKind {
	if evidence == nil {
		return IdentityUnspecified
	}
	switch evidence.GetIdentity().(type) {
	case *capabilityv1.CapabilityEvidence_Config:
		return IdentityConfig
	case *capabilityv1.CapabilityEvidence_Boot:
		return IdentityBoot
	case *capabilityv1.CapabilityEvidence_Mount:
		return IdentityMount
	case *capabilityv1.CapabilityEvidence_Runtime:
		return IdentityRuntime
	case *capabilityv1.CapabilityEvidence_Derived:
		return IdentityDerived
	default:
		return IdentityUnspecified
	}
}

func ValidateEvidence(evidence *capabilityv1.CapabilityEvidence, expected IdentityKind) error {
	if evidence == nil || evidence.GetIdentity() == nil {
		return fmt.Errorf("typed evidence identity is required")
	}
	if kind := EvidenceIdentityKind(evidence); kind != expected {
		return fmt.Errorf("evidence identity kind %d does not match catalog kind %d", kind, expected)
	}
	if evidence.GetEvidenceID() == "" || evidence.GetEvidenceID() != EvidenceID(evidence) {
		return fmt.Errorf("evidence_id does not match typed identity")
	}
	validateIdentity := validateBoundedIdentity
	validateDigest := func(name, value string) error {
		if !digestPattern.MatchString(value) {
			return fmt.Errorf("%s must be a sha256 digest", name)
		}
		return nil
	}
	switch identity := evidence.GetIdentity().(type) {
	case *capabilityv1.CapabilityEvidence_Config:
		return validateDigest("config_digest", identity.Config.GetConfigDigest())
	case *capabilityv1.CapabilityEvidence_Boot:
		return validateBootID(identity.Boot.GetBootID())
	case *capabilityv1.CapabilityEvidence_Mount:
		if err := validateBootID(identity.Mount.GetBootID()); err != nil {
			return err
		}
		return validateIdentity("mount_identity", identity.Mount.GetMountIdentity())
	case *capabilityv1.CapabilityEvidence_Runtime:
		if err := validateBootID(identity.Runtime.GetBootID()); err != nil {
			return err
		}
		switch identity.Runtime.GetRuntimeName() {
		case "runc", "runsc":
		default:
			return fmt.Errorf("runtime_name must be runc or runsc")
		}
		if err := validateDigest("runtime_binary_digest", identity.Runtime.GetRuntimeBinaryDigest()); err != nil {
			return err
		}
		return validateDigest("runtime_config_digest", identity.Runtime.GetRuntimeConfigDigest())
	case *capabilityv1.CapabilityEvidence_Derived:
		if identity.Derived.GetCatalogDigest() != CatalogDigest() {
			return fmt.Errorf("derived catalog digest does not match current catalog")
		}
		return validateDigest("dependency_evidence_digest", identity.Derived.GetDependencyEvidenceDigest())
	default:
		return fmt.Errorf("unsupported evidence identity")
	}
}

func validateBootID(value string) error {
	if !bootIDPattern.MatchString(value) {
		return fmt.Errorf("boot_id must be a canonical lowercase UUID")
	}
	return nil
}

func validateDerivedEvidenceDependencies(evidence *capabilityv1.CapabilityEvidence, dependencies []*capabilityv1.CapabilityObservationProof) error {
	derived := evidence.GetDerived()
	if derived == nil {
		return fmt.Errorf("derived evidence identity is required")
	}
	expected := dependencyEvidenceDigest(dependencies)
	if expected == "" || derived.GetDependencyEvidenceDigest() != expected {
		return fmt.Errorf("derived evidence does not match dependency evidence identities")
	}
	return nil
}

func NewObservationProof(observation *capabilityv1.CapabilityObservation) *capabilityv1.CapabilityObservationProof {
	if observation == nil {
		return nil
	}
	proof := &capabilityv1.CapabilityObservationProof{
		Key:           CloneKey(observation.GetKey()),
		ObservationID: observation.GetObservationID(),
		Provider:      observation.GetProvider(),
		ObservedAt:    cloneTimestamp(observation.GetObservedAt()),
		ValidUntil:    cloneTimestamp(observation.GetValidUntil()),
	}
	if observation.GetEvidence() != nil {
		proof.Evidence = proto.Clone(observation.GetEvidence()).(*capabilityv1.CapabilityEvidence)
	}
	return proof
}

func cloneTimestamp(in *timestamppb.Timestamp) *timestamppb.Timestamp {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*timestamppb.Timestamp)
}

func ValidateObservationProof(proof *capabilityv1.CapabilityObservationProof, now time.Time) error {
	return validateObservationProof(proof, now, true)
}

func validateObservationProof(proof *capabilityv1.CapabilityObservationProof, at time.Time, requireCurrent bool) error {
	if proof == nil || proof.GetObservationID() == "" || proof.GetObservedAt() == nil {
		return fmt.Errorf("observation proof identity and observed_at are required")
	}
	if !digestPattern.MatchString(proof.GetObservationID()) || proof.GetObservationID() != observationProofID(proof) {
		return fmt.Errorf("observation proof observation_id does not match its canonical proof")
	}
	if err := proof.GetObservedAt().CheckValid(); err != nil {
		return fmt.Errorf("observation proof observed_at: %w", err)
	}
	if proof.GetValidUntil() != nil {
		if err := proof.GetValidUntil().CheckValid(); err != nil {
			return fmt.Errorf("observation proof valid_until: %w", err)
		}
	}
	provider, identity, err := ObservationOwner(proof.GetKey())
	if err != nil {
		return err
	}
	if proof.GetProvider() != provider {
		return fmt.Errorf("observation proof provider does not match catalog owner")
	}
	if err := ValidateEvidence(proof.GetEvidence(), identity); err != nil {
		return err
	}
	if proof.GetObservedAt().AsTime().After(at.Add(time.Minute)) {
		return fmt.Errorf("observation proof is from the future")
	}
	return validateFreshness(proof.GetKey(), proof.GetObservedAt(), proof.GetValidUntil(), at, requireCurrent)
}

func validateFreshness(key *capabilityv1.CapabilityKey, observedAt, validUntil *timestamppb.Timestamp, now time.Time, requireCurrent bool) error {
	var maxValidity time.Duration
	if extension := key.GetExtension(); extension == nil {
		definition, ok := PlatformDefinition(key.GetPlatform())
		if !ok {
			return fmt.Errorf("unknown platform capability %d", key.GetPlatform())
		}
		if definition.Identity == IdentityDerived {
			if validUntil == nil {
				return fmt.Errorf("derived observation is missing inherited valid_until")
			}
			if !validUntil.AsTime().After(observedAt.AsTime()) {
				return fmt.Errorf("derived valid_until must be after observed_at")
			}
			if requireCurrent && !validUntil.AsTime().After(now) {
				return fmt.Errorf("derived observation proof is expired")
			}
			return nil
		}
		maxValidity = definition.Freshness.MaxValidity
	}
	if maxValidity == 0 {
		if validUntil != nil {
			return fmt.Errorf("non-expiring observation cannot set valid_until")
		}
		return nil
	}
	if validUntil == nil {
		return fmt.Errorf("expiring observation is missing valid_until")
	}
	observed, expires := observedAt.AsTime(), validUntil.AsTime()
	if !expires.After(observed) || expires.After(observed.Add(maxValidity)) {
		return fmt.Errorf("valid_until is outside the catalog freshness bound")
	}
	if requireCurrent && !expires.After(now) {
		return fmt.Errorf("observation proof is expired")
	}
	return nil
}

func ObservationID(observation *capabilityv1.CapabilityObservation) string {
	if observation == nil {
		return ""
	}
	proof := &capabilityv1.CapabilityObservationProof{
		Key:        CloneKey(observation.GetKey()),
		Provider:   observation.GetProvider(),
		ObservedAt: cloneTimestamp(observation.GetObservedAt()),
		ValidUntil: cloneTimestamp(observation.GetValidUntil()),
	}
	if observation.GetEvidence() != nil {
		proof.Evidence = proto.Clone(observation.GetEvidence()).(*capabilityv1.CapabilityEvidence)
	}
	return observationProofID(proof)
}

// observationProofID is intentionally derivable from the durable proof
// itself. State, diagnostics, and dependency edges have their own typed
// validation and are not smuggled into an unverifiable opaque identifier.
func observationProofID(proof *capabilityv1.CapabilityObservationProof) string {
	if proof == nil {
		return ""
	}
	clone := proto.Clone(proof).(*capabilityv1.CapabilityObservationProof)
	clone.ObservationID = ""
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(clone)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func NormalizeObservation(observation *capabilityv1.CapabilityObservation) {
	if observation == nil {
		return
	}
	observation.Reason = BoundedReason(strings.TrimSpace(observation.GetReason()))
	if observation.GetEvidence() != nil {
		observation.Evidence.EvidenceID = EvidenceID(observation.GetEvidence())
	}
	observation.ObservationID = ObservationID(observation)
}
