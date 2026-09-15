package pgnodes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type nodeUpsertParams struct {
	NodeID        string
	NodeTarget    string
	Summary       *nodev1.NodeSummary
	NodeAuthToken string
	Now           time.Time
}

const maxNodeObservationInstanceIDBytes = 128

func (s *PGStore) upsert(ctx context.Context, params nodeUpsertParams) (*nodekernel.Record, error) {
	nodeID := strings.TrimSpace(params.NodeID)
	if params.Summary != nil {
		if err := validateSummaryPublication(params.Summary, params.Now); err != nil {
			return nil, err
		}
	}
	nodeAuthToken := strings.TrimSpace(params.NodeAuthToken)
	if nodeAuthToken == "" {
		return nil, grpcstatus.Error(codes.PermissionDenied, "node auth token is required")
	}
	tokenHash := hashNodeAuthToken(nodeAuthToken)
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin node tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var existingHash, lifecycle string
	err = tx.QueryRow(ctx, `SELECT node_auth_token_hash, lifecycle_status FROM nodes WHERE node_id = $1 FOR UPDATE`, nodeID).Scan(&existingHash, &lifecycle)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load node auth token: %w", err)
	}
	if err == nil {
		if lifecycle == string(nodekernel.LifecycleRetired) {
			return nil, grpcstatus.Error(codes.FailedPrecondition, "node is retired")
		}
		if existingHash == "" || tokenHash != existingHash {
			return nil, grpcstatus.Error(codes.PermissionDenied, "invalid node auth token")
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO nodes (
			node_id, node_target, registered_at, last_heartbeat_at,
			node_auth_token_hash, lifecycle_status
		) VALUES ($1, $2, $3, $3, $4, 'active')
		ON CONFLICT (node_id) DO UPDATE SET
			node_target = EXCLUDED.node_target,
			last_heartbeat_at = GREATEST(nodes.last_heartbeat_at, EXCLUDED.last_heartbeat_at),
			node_auth_token_hash = EXCLUDED.node_auth_token_hash
	`, nodeID, params.NodeTarget, params.Now.UTC(), tokenHash); err != nil {
		return nil, fmt.Errorf("upsert node: %w", err)
	}

	var reportedTransitions []nodekernel.CapabilityChange
	if params.Summary != nil {
		var previous *nodev1.NodeSummary
		var previousJSON []byte
		if err := tx.QueryRow(ctx, `SELECT summary FROM node_summaries WHERE node_id = $1 FOR UPDATE`, nodeID).Scan(&previousJSON); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("load previous node summary: %w", err)
		} else if err == nil {
			previous = &nodev1.NodeSummary{}
			if err := protojson.Unmarshal(previousJSON, previous); err != nil {
				return nil, fmt.Errorf("unmarshal previous node summary: %w", err)
			}
		}
		if _, err := validateNodeObservationAdvance(previous, params.Summary); err != nil {
			return nil, err
		}
		if err := persistNodeObservationInstance(ctx, tx, nodeID, previous, params.Summary); err != nil {
			return nil, err
		}
		transitions, err := persistCapabilityReport(ctx, tx, nodeID, previous, params.Summary, params.Now)
		if err != nil {
			return nil, err
		}
		for _, transition := range transitions {
			reportedTransitions = append(reportedTransitions, nodekernel.CapabilityChange{
				Key:        capabilityKeyClone(transition.key),
				NewState:   transition.newState,
				ReasonCode: transition.reasonCode,
			})
		}
		payload, err := protojson.Marshal(params.Summary)
		if err != nil {
			return nil, fmt.Errorf("marshal node summary: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO node_summaries(node_id, summary)
			VALUES ($1, $2::jsonb)
			ON CONFLICT (node_id) DO UPDATE SET
				summary = EXCLUDED.summary
		`, nodeID, string(payload)); err != nil {
			return nil, fmt.Errorf("upsert node summary: %w", err)
		}
	}

	record, err := loadNodeRecord(ctx, tx, nodeID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit node tx: %w", err)
	}
	record.ReportedCapabilityChanges = reportedTransitions
	return record, nil
}

func validateSummaryPublication(summary *nodev1.NodeSummary, reportedAt time.Time) error {
	if summary == nil || summary.GetCollectedAt() == nil {
		return fmt.Errorf("node summary collected_at is required")
	}
	if err := summary.GetCollectedAt().CheckValid(); err != nil {
		return fmt.Errorf("node summary collected_at: %w", err)
	}
	if reportedAt.IsZero() {
		return fmt.Errorf("node summary report time is required")
	}
	collectedAt := summary.GetCollectedAt().AsTime()
	if collectedAt.After(reportedAt.Add(time.Minute)) {
		return fmt.Errorf("node summary collected_at is in the future")
	}
	instanceID := summary.GetNodeInstanceID()
	if strings.TrimSpace(instanceID) == "" || instanceID != strings.TrimSpace(instanceID) || !utf8.ValidString(instanceID) || len(instanceID) > maxNodeObservationInstanceIDBytes {
		return fmt.Errorf("node summary node_instance_id must be a non-empty UTF-8 identity of at most %d bytes without surrounding whitespace", maxNodeObservationInstanceIDBytes)
	}
	if summary.GetSequence() <= 0 {
		return fmt.Errorf("node summary sequence must be positive")
	}
	snapshot := summary.GetCapabilitySnapshot()
	if snapshot == nil || snapshot.GetCollectedAt() == nil {
		return fmt.Errorf("node summary capability snapshot and collected_at are required")
	}
	if snapshot.GetCollectedAt().AsTime().After(collectedAt) {
		return fmt.Errorf("capability snapshot was published after its enclosing node summary")
	}
	return nil
}

func capabilityKeyClone(key *capabilityv1.CapabilityKey) *capabilityv1.CapabilityKey {
	if key == nil {
		return nil
	}
	return proto.Clone(key).(*capabilityv1.CapabilityKey)
}
