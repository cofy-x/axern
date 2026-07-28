package rolloutworkerv1

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"
	"time"

	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Dependencies struct {
	Now            func() time.Time
	Store          rolloutkernel.WorkerStore
	BootstrapToken string
	SessionTTL     time.Duration
	LeaseTTL       time.Duration
}

type Server struct {
	workerrolloutv1.UnimplementedRolloutWorkerControlServer
	deps Dependencies
}

func New(deps Dependencies) *Server {
	if deps.SessionTTL <= 0 {
		deps.SessionTTL = rolloutkernel.DefaultWorkerSession
	}
	if deps.LeaseTTL <= 0 {
		deps.LeaseTTL = rolloutkernel.DefaultWorkLease
	}
	return &Server{deps: deps}
}

func (s *Server) RegisterWorker(ctx context.Context, req *workerrolloutv1.RegisterWorkerRequest) (*workerrolloutv1.RegisterWorkerResponse, error) {
	if !equalToken(req.GetAuthToken(), s.deps.BootstrapToken) {
		return nil, status.Error(codes.Unauthenticated, "invalid worker bootstrap credential")
	}
	sanitized := proto.Clone(req).(*workerrolloutv1.RegisterWorkerRequest)
	sanitized.AuthToken = ""
	return s.deps.Store.RegisterWorker(ctx, sanitized, rolloutkernel.HashToken(s.deps.BootstrapToken), s.deps.Now(), s.deps.SessionTTL)
}
func (s *Server) ClaimWork(ctx context.Context, req *workerrolloutv1.ClaimWorkRequest) (*workerrolloutv1.ClaimWorkResponse, error) {
	work, token, err := s.deps.Store.ClaimWork(ctx, strings.TrimSpace(req.GetSessionID()), rolloutkernel.HashToken(req.GetSessionToken()), s.deps.Now(), s.deps.LeaseTTL)
	if err != nil {
		return nil, err
	}
	longPoll := time.Duration(0)
	if req.GetLongPoll() != nil {
		longPoll = req.GetLongPoll().AsDuration()
	}
	if work == nil && longPoll > 0 {
		if longPoll > 30*time.Second {
			longPoll = 30 * time.Second
		}
		if err := s.deps.Store.WaitForWork(ctx, strings.TrimSpace(req.GetSessionID()), rolloutkernel.HashToken(req.GetSessionToken()), longPoll); err != nil {
			return nil, err
		}
		work, token, err = s.deps.Store.ClaimWork(ctx, strings.TrimSpace(req.GetSessionID()), rolloutkernel.HashToken(req.GetSessionToken()), s.deps.Now(), s.deps.LeaseTTL)
		if err != nil {
			return nil, err
		}
	}
	return &workerrolloutv1.ClaimWorkResponse{Work: work, LeaseToken: token}, nil
}
func (s *Server) RenewWorkLease(ctx context.Context, req *workerrolloutv1.RenewWorkLeaseRequest) (*workerrolloutv1.RenewWorkLeaseResponse, error) {
	expires, cancel, err := s.deps.Store.RenewWork(ctx, req.GetWorkID(), rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now(), s.deps.LeaseTTL)
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.RenewWorkLeaseResponse{ExpiresAt: timestamppb.New(expires), CancelRequested: cancel}, nil
}
func (s *Server) ReportWorkProgress(ctx context.Context, req *workerrolloutv1.ReportWorkProgressRequest) (*workerrolloutv1.ReportWorkProgressResponse, error) {
	cancel, err := s.deps.Store.ReportProgress(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.ReportWorkProgressResponse{CancelRequested: cancel}, nil
}
func (s *Server) CompletePlan(ctx context.Context, req *workerrolloutv1.CompletePlanRequest) (*workerrolloutv1.CompletePlanResponse, error) {
	rollout, err := s.deps.Store.CompletePlan(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.CompletePlanResponse{Rollout: rollout}, nil
}
func (s *Server) CompleteEpisode(ctx context.Context, req *workerrolloutv1.CompleteEpisodeRequest) (*workerrolloutv1.CompleteEpisodeResponse, error) {
	rollout, err := s.deps.Store.CompleteEpisode(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.CompleteEpisodeResponse{Rollout: rollout}, nil
}
func (s *Server) CompleteProfileDoctor(ctx context.Context, req *workerrolloutv1.CompleteProfileDoctorRequest) (*workerrolloutv1.CompleteProfileDoctorResponse, error) {
	if err := s.deps.Store.CompleteProfileDoctor(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now()); err != nil {
		return nil, err
	}
	return &workerrolloutv1.CompleteProfileDoctorResponse{}, nil
}
func (s *Server) FailWork(ctx context.Context, req *workerrolloutv1.FailWorkRequest) (*workerrolloutv1.FailWorkResponse, error) {
	rollout, err := s.deps.Store.FailWork(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.FailWorkResponse{Rollout: rollout}, nil
}

func (s *Server) ResolveAgentProfile(ctx context.Context, req *workerrolloutv1.ResolveAgentProfileRequest) (*workerrolloutv1.ResolveAgentProfileResponse, error) {
	profile, err := s.deps.Store.ResolveAgentProfile(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.ResolveAgentProfileResponse{Profile: profile}, nil
}
func (s *Server) ReserveUsage(ctx context.Context, req *workerrolloutv1.ReserveUsageRequest) (*workerrolloutv1.ReserveUsageResponse, error) {
	tokens, cost, err := s.deps.Store.ReserveUsage(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.ReserveUsageResponse{RemainingTokens: tokens, RemainingCostMicrousd: cost}, nil
}
func (s *Server) CommitUsage(ctx context.Context, req *workerrolloutv1.CommitUsageRequest) (*workerrolloutv1.CommitUsageResponse, error) {
	tokens, cost, err := s.deps.Store.CommitUsage(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.CommitUsageResponse{RemainingTokens: tokens, RemainingCostMicrousd: cost}, nil
}
func (s *Server) ReleaseUsage(ctx context.Context, req *workerrolloutv1.ReleaseUsageRequest) (*workerrolloutv1.ReleaseUsageResponse, error) {
	if err := s.deps.Store.ReleaseUsage(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now()); err != nil {
		return nil, err
	}
	return &workerrolloutv1.ReleaseUsageResponse{}, nil
}
func (s *Server) CreateArtifactUpload(ctx context.Context, req *workerrolloutv1.CreateArtifactUploadRequest) (*workerrolloutv1.CreateArtifactUploadResponse, error) {
	artifact, upload, err := s.deps.Store.CreateArtifactUpload(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now(), 15*time.Minute)
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.CreateArtifactUploadResponse{Artifact: artifact, UploadUrl: upload.URL, ExpiresAt: timestamppb.New(upload.ExpiresAt), Headers: upload.Headers}, nil
}
func (s *Server) CommitArtifact(ctx context.Context, req *workerrolloutv1.CommitArtifactRequest) (*workerrolloutv1.CommitArtifactResponse, error) {
	artifact, err := s.deps.Store.CommitArtifact(ctx, req, rolloutkernel.HashToken(req.GetLeaseToken()), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workerrolloutv1.CommitArtifactResponse{Artifact: artifact}, nil
}

func equalToken(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	a := sha256.Sum256([]byte(got))
	b := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
