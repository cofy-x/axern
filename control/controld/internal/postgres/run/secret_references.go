package pgrun

import (
	"context"
	"fmt"
	"sort"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func insertEnvironmentSecretReference(ctx context.Context, tx pgx.Tx, environment *environmentv1.Environment) error {
	secretID := strings.TrimSpace(environment.GetSpec().GetImage().GetRegistryCredentialID())
	if secretID == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO environment_secret_references (environment_id, namespace, secret_id)
		SELECT $1, $2, secret_id
		FROM secrets
		WHERE secret_id = $3 AND namespace = $2 AND type = 'SECRET_TYPE_DOCKER_CONFIG_JSON'
	`, environment.GetID(), environment.GetNamespace(), secretID)
	if err != nil {
		return fmt.Errorf("insert environment registry credential reference: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return grpcstatus.Errorf(codes.FailedPrecondition, "registry credential %q is not a docker config Secret in namespace %q", secretID, environment.GetNamespace())
	}
	return nil
}

func insertRunSecretReferences(ctx context.Context, tx pgx.Tx, run *runv1.Run) error {
	for _, secretID := range requiredRunSecretIDs(run.GetConfig(), run.GetEnvironmentSpec()) {
		tag, err := tx.Exec(ctx, `
			INSERT INTO run_secret_references (run_id, namespace, secret_id)
			SELECT $1, $2, secret_id
			FROM secrets
			WHERE secret_id = $3 AND namespace = $2
		`, run.GetID(), run.GetNamespace(), secretID)
		if err != nil {
			return fmt.Errorf("insert run Secret reference: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return grpcstatus.Errorf(codes.FailedPrecondition, "required Secret %q does not exist in namespace %q", secretID, run.GetNamespace())
		}
	}
	return nil
}

func requiredRunSecretIDs(config *commonv1.ExecutionConfig, environmentSpec *environmentv1.EnvironmentSpec) []string {
	ids := map[string]struct{}{}
	if secretID := strings.TrimSpace(environmentSpec.GetImage().GetRegistryCredentialID()); secretID != "" {
		ids[secretID] = struct{}{}
	}
	for _, ref := range config.GetSecretEnv() {
		if ref != nil && !ref.GetOptional() {
			ids[strings.TrimSpace(ref.GetSecretID())] = struct{}{}
		}
	}
	for _, ref := range config.GetSecretFiles() {
		if ref != nil && !ref.GetOptional() {
			ids[strings.TrimSpace(ref.GetSecretID())] = struct{}{}
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		if id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func deleteRunSecretReferences(ctx context.Context, tx pgx.Tx, runID string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM run_secret_references WHERE run_id = $1`, strings.TrimSpace(runID)); err != nil {
		return fmt.Errorf("delete run Secret references: %w", err)
	}
	return nil
}

func deleteAllocationRunSecretReferences(ctx context.Context, tx pgx.Tx, allocationID string) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM run_secret_references
		WHERE run_id = (SELECT run_id FROM allocations WHERE allocation_id = $1)
	`, strings.TrimSpace(allocationID)); err != nil {
		return fmt.Errorf("delete allocation Run Secret references: %w", err)
	}
	return nil
}
