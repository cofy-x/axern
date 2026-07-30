package identityv1

import (
	"context"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	identityv1.UnimplementedIdentityControlServer
}

func New() *Server { return &Server{} }

func (*Server) WhoAmI(ctx context.Context, _ *identityv1.WhoAmIRequest) (*identityv1.WhoAmIResponse, error) {
	actor, ok := accesskernel.ActorFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authenticated principal is required")
	}
	roles := make([]*identityv1.EffectiveRole, 0, len(actor.Bindings))
	for _, binding := range actor.Bindings {
		roles = append(roles, &identityv1.EffectiveRole{Role: string(binding.Role), ScopeType: string(binding.Scope), Namespace: binding.Namespace})
	}
	return &identityv1.WhoAmIResponse{
		Principal:  &identityv1.PrincipalIdentity{PrincipalID: actor.Principal.ID, Name: actor.Principal.Name, DisplayName: actor.Principal.DisplayName, Kind: string(actor.Principal.Kind)},
		Credential: &identityv1.CredentialIdentity{CredentialID: actor.Credential.ID, Label: actor.Credential.Label, Fingerprint: accesskernel.FormatFingerprint(actor.Credential.Fingerprint), CertificateNotAfter: timestamppb.New(actor.Credential.CertificateNotAfter)},
		Roles:      roles,
	}, nil
}
