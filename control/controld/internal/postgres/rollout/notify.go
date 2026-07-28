package pgrollout

import (
	"context"
	"errors"
	"time"

	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) WaitForEvents(ctx context.Context, rolloutID string, afterSequence int64, timeout time.Duration) error {
	subscription, err := s.notifications.subscribeEvent(ctx, rolloutID)
	if err != nil {
		return notificationWaitError(err)
	}
	defer subscription.close()
	var ready bool
	if err := s.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM rollout_events WHERE rollout_id=$1 AND sequence>$2)`, rolloutID, afterSequence).Scan(&ready); err != nil {
		return err
	}
	if ready {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := subscription.wait(waitCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil
		}
		return notificationWaitError(err)
	}
	return nil
}

func (s *Store) WaitForWork(ctx context.Context, sessionID, sessionTokenHash string, timeout time.Duration) error {
	session, err := s.loadWorkWaitSession(ctx, sessionID, sessionTokenHash, s.now().UTC())
	if err != nil {
		return err
	}
	subscription, err := s.notifications.subscribeWork(ctx, session.selector)
	if err != nil {
		return notificationWaitError(err)
	}
	defer subscription.close()
	var ready bool
	if err := s.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 `+claimableWorkFromSQL+`)`,
		s.now().UTC(), session.selector.planner, session.selector.agents, session.workerID, session.maxConcurrency, session.selector.wireAPIs,
	).Scan(&ready); err != nil {
		return err
	}
	if ready {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, jitteredWorkWait(timeout, subscription.id))
	defer cancel()
	if err := subscription.wait(waitCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil
		}
		return notificationWaitError(err)
	}
	return nil
}

type workWaitSession struct {
	workerID       string
	maxConcurrency int32
	selector       workWaitSelector
}

func (s *Store) loadWorkWaitSession(ctx context.Context, sessionID, sessionTokenHash string, now time.Time) (workWaitSession, error) {
	var workerID string
	var capabilitiesJSON []byte
	var expiresAt time.Time
	if err := s.db.Pool().QueryRow(ctx, `SELECT worker_id,capabilities,expires_at FROM rollout_worker_sessions WHERE session_id=$1 AND token_hash=$2`, sessionID, sessionTokenHash).Scan(&workerID, &capabilitiesJSON, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return workWaitSession{}, status.Error(codes.Unauthenticated, "invalid worker session")
		}
		return workWaitSession{}, err
	}
	if !expiresAt.After(now) {
		return workWaitSession{}, status.Error(codes.Unauthenticated, "worker session expired")
	}
	capabilities := &workerrolloutv1.WorkerCapabilities{}
	if err := protojson.Unmarshal(capabilitiesJSON, capabilities); err != nil {
		return workWaitSession{}, err
	}
	wireAPIs := make([]string, 0, len(capabilities.GetWireApis()))
	for _, wireAPI := range capabilities.GetWireApis() {
		wireAPIs = append(wireAPIs, wireAPI.String())
	}
	return workWaitSession{
		workerID:       workerID,
		maxConcurrency: capabilities.GetMaxConcurrency(),
		selector:       newWorkWaitSelector(capabilities.GetPlanner(), capabilities.GetAgents(), wireAPIs),
	}, nil
}

func jitteredWorkWait(timeout time.Duration, subscriptionID uint64) time.Duration {
	if timeout <= 0 {
		return timeout
	}
	percent := 80 + subscriptionID%21
	return time.Duration(int64(timeout) * int64(percent) / 100)
}

func notificationWaitError(err error) error {
	if errors.Is(err, errNotificationListenerUnavailable) {
		return status.Error(codes.Unavailable, "rollout notification listener is temporarily unavailable")
	}
	return err
}
