package pgrun

import (
	"context"
	"fmt"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) CreateEnvironment(ctx context.Context, params runkernel.CreateEnvironmentParams, now time.Time) (*environmentv1.Environment, error) {
	normalized := cloneEnvironmentSpec(params.Spec)
	if normalized == nil {
		normalized = &environmentv1.EnvironmentSpec{}
	}
	normalized.Namespace = environmentkernel.NormalizeNamespace(normalized.GetNamespace())
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create environment tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := pgnamespace.Ensure(ctx, tx, normalized.GetNamespace()); err != nil {
		return nil, err
	}

	env := &environmentv1.Environment{
		ID:           "env-" + uuid.NewString(),
		Namespace:    normalized.GetNamespace(),
		Spec:         normalized,
		ResolvedSpec: params.ResolvedSpec,
		Labels:       runkernel.CloneLabels(params.Labels),
		CreatedAt:    timestamppb.New(now),
	}
	specJSON, err := marshalProtoJSON(normalized)
	if err != nil {
		return nil, err
	}
	resolvedSpecJSON, err := marshalProtoJSON(params.ResolvedSpec)
	if err != nil {
		return nil, err
	}
	labelsJSON, err := marshalJSONMap(params.Labels)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO environments (
			environment_id, namespace, spec, resolved_spec, labels, created_at
		) VALUES ($1, $2, $3::jsonb, $4::jsonb, $5::jsonb, $6)
	`, env.GetID(), env.GetNamespace(), specJSON, resolvedSpecJSON, labelsJSON, now.UTC()); err != nil {
		return nil, fmt.Errorf("insert environment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create environment tx: %w", err)
	}
	return env, nil
}

func cloneEnvironmentSpec(spec *environmentv1.EnvironmentSpec) *environmentv1.EnvironmentSpec {
	if spec == nil {
		return nil
	}
	cloned, ok := proto.Clone(spec).(*environmentv1.EnvironmentSpec)
	if !ok || cloned == nil {
		return nil
	}
	return cloned
}
