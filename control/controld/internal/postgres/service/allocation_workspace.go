package pgservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *PGStore) RecordWorkspacePreparation(
	ctx context.Context,
	serviceID string,
	allocationID string,
	attempt int64,
	facts *commonv1.WorkspacePreparationFacts,
	now time.Time,
) error {
	factsJSON := []byte("null")
	if facts != nil {
		var err error
		factsJSON, err = protojson.Marshal(facts)
		if err != nil {
			return fmt.Errorf("marshal workspace preparation: %w", err)
		}
	}
	result, err := s.db.Pool().Exec(ctx, `
		UPDATE allocations
		SET workspace_preparation = $5::jsonb, updated_at = $6, version = version + 1
		WHERE owner_type = $1 AND owner_id = $2 AND allocation_id = $3 AND attempt = $4
	`, allocationOwnerService, strings.TrimSpace(serviceID), strings.TrimSpace(allocationID), attempt, factsJSON, now.UTC())
	if err != nil {
		return fmt.Errorf("record workspace preparation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("record workspace preparation: allocation %s attempt %d not found", strings.TrimSpace(allocationID), attempt)
	}
	return nil
}
