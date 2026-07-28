package publicv1

import (
	"context"
	"strings"

	agentprofilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/agentprofile"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) CreateAgentProfile(ctx context.Context, req *agentprofilev1.CreateAgentProfileRequest) (*agentprofilev1.CreateAgentProfileResponse, error) {
	if s.deps.AgentProfiles == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent profile control is not configured")
	}
	profile, err := s.deps.AgentProfiles.Create(ctx, agentprofilekernel.CreateParams{Namespace: req.GetNamespace(), Name: req.GetName(), Spec: req.GetSpec(), Labels: req.GetLabels(), Credential: req.GetCredential(), IdempotencyKey: req.GetIdempotencyKey()}, s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &agentprofilev1.CreateAgentProfileResponse{Profile: profile}, nil
}

func (s *Server) GetAgentProfile(ctx context.Context, req *agentprofilev1.GetAgentProfileRequest) (*agentprofilev1.GetAgentProfileResponse, error) {
	namespace, name := strings.TrimSpace(req.GetNamespace()), strings.TrimSpace(req.GetName())
	if namespace == "" || name == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace and name are required")
	}
	profile, ok, err := s.deps.AgentProfiles.Get(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "agent profile not found")
	}
	return &agentprofilev1.GetAgentProfileResponse{Profile: profile}, nil
}
func (s *Server) UpdateAgentProfile(ctx context.Context, req *agentprofilev1.UpdateAgentProfileRequest) (*agentprofilev1.UpdateAgentProfileResponse, error) {
	profile, err := s.deps.AgentProfiles.Update(ctx, agentprofilekernel.UpdateParams{Namespace: req.GetNamespace(), Name: req.GetName(), Patch: req.GetPatch(), ExpectedVersion: req.GetExpectedVersion(), IdempotencyKey: req.GetIdempotencyKey()}, s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &agentprofilev1.UpdateAgentProfileResponse{Profile: profile}, nil
}
func (s *Server) RotateAgentProfileCredential(ctx context.Context, req *agentprofilev1.RotateAgentProfileCredentialRequest) (*agentprofilev1.RotateAgentProfileCredentialResponse, error) {
	profile, err := s.deps.AgentProfiles.Rotate(ctx, agentprofilekernel.RotateParams{Namespace: req.GetNamespace(), Name: req.GetName(), Credential: req.GetCredential(), ExpectedVersion: req.GetExpectedVersion(), IdempotencyKey: req.GetIdempotencyKey()}, s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &agentprofilev1.RotateAgentProfileCredentialResponse{Profile: profile}, nil
}
func (s *Server) DoctorAgentProfile(ctx context.Context, req *agentprofilev1.DoctorAgentProfileRequest) (*agentprofilev1.DoctorAgentProfileResponse, error) {
	return s.deps.AgentProfiles.Doctor(ctx, agentprofilekernel.DoctorParams{Namespace: req.GetNamespace(), Name: req.GetName(), Model: req.GetModel()}, s.deps.Now())
}
func (s *Server) ListAgentProfiles(ctx context.Context, req *agentprofilev1.ListAgentProfilesRequest) (*agentprofilev1.ListAgentProfilesResponse, error) {
	profiles, next, err := s.deps.AgentProfiles.List(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	return &agentprofilev1.ListAgentProfilesResponse{Profiles: profiles, NextCursor: next}, nil
}
func (s *Server) DeleteAgentProfile(ctx context.Context, req *agentprofilev1.DeleteAgentProfileRequest) (*agentprofilev1.DeleteAgentProfileResponse, error) {
	namespace, name := strings.TrimSpace(req.GetNamespace()), strings.TrimSpace(req.GetName())
	if namespace == "" || name == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace and name are required")
	}
	profile, ok, err := s.deps.AgentProfiles.Delete(ctx, namespace, name, req.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "agent profile not found")
	}
	return &agentprofilev1.DeleteAgentProfileResponse{Profile: profile}, nil
}
