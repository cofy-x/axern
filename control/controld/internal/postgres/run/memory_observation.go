package pgrun

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

// BatchReportAllocationMemoryObservations stores one latest, revision-fenced
// host-kernel observation for every allocation regardless of its owner type.
func (s *Store) BatchReportAllocationMemoryObservations(ctx context.Context, nodeID string, observations []*nodev1.AllocationMemoryObservation, now time.Time) error {
	ordered := append([]*nodev1.AllocationMemoryObservation(nil), observations...)
	sort.Slice(ordered, func(i, j int) bool {
		return strings.TrimSpace(ordered[i].GetAllocationID()) < strings.TrimSpace(ordered[j].GetAllocationID())
	})
	return s.withTx(ctx, func(tx pgx.Tx) error {
		for _, observation := range ordered {
			allocationID := strings.TrimSpace(observation.GetAllocationID())
			var admittedNodeID string
			var attempt int64
			err := tx.QueryRow(ctx, `
				SELECT node_id, attempt FROM allocations
				WHERE allocation_id = $1
				FOR UPDATE
			`, allocationID).Scan(&admittedNodeID, &attempt)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return fmt.Errorf("lock allocation %q memory observation: %w", allocationID, err)
			}
			if admittedNodeID != strings.TrimSpace(nodeID) || attempt != observation.GetAttempt() {
				continue
			}
			payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(observation)
			if err != nil {
				return fmt.Errorf("marshal allocation %q memory observation: %w", allocationID, err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO allocation_memory_observations (
					allocation_id, allocation_attempt, node_id, revision, observed_at, observation, updated_at
				) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
				ON CONFLICT (allocation_id) DO UPDATE SET
					allocation_attempt = EXCLUDED.allocation_attempt,
					node_id = EXCLUDED.node_id,
					revision = EXCLUDED.revision,
					observed_at = EXCLUDED.observed_at,
					observation = EXCLUDED.observation,
					updated_at = EXCLUDED.updated_at
				WHERE allocation_memory_observations.allocation_attempt < EXCLUDED.allocation_attempt
				   OR (allocation_memory_observations.allocation_attempt = EXCLUDED.allocation_attempt
				       AND allocation_memory_observations.revision < EXCLUDED.revision)
			`, allocationID, attempt, admittedNodeID, observation.GetRevision(), observation.GetObservedAt().AsTime(), payload, now); err != nil {
				return fmt.Errorf("upsert allocation %q memory observation: %w", allocationID, err)
			}
		}
		return nil
	})
}
