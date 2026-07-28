package pgfunction

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/jackc/pgx/v5"
)

func (s *Store) getFunction(ctx context.Context, functionID, namespace, name string) (*functionv1.Function, *functionv1.FunctionRevision, *functionv1.FunctionDeployment, bool, error) {
	return getFunctionTx(ctx, s.db.Pool(), functionID, namespace, name, false)
}

func getFunctionTx(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, functionID, namespace, name string, forUpdate bool) (*functionv1.Function, *functionv1.FunctionRevision, *functionv1.FunctionDeployment, bool, error) {
	functionID = strings.TrimSpace(functionID)
	namespace = functionkernel.NormalizeNamespace(namespace)
	name = functionkernel.NormalizeName(name)
	var (
		fn  *functionv1.Function
		err error
	)
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	if functionID != "" {
		fn, err = scanFunction(q.QueryRow(ctx, functionSelectSQL()+` WHERE function_id = $1`+lock, functionID))
	} else if name != "" {
		fn, err = scanFunction(q.QueryRow(ctx, functionSelectSQL()+` WHERE namespace = $1 AND name = $2`+lock, namespace, name))
	} else {
		return nil, nil, nil, false, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("get function: %w", err)
	}
	revision, err := scanRevision(q.QueryRow(ctx, revisionSelectSQL()+` WHERE revision_id = $1`, fn.GetActiveRevisionID()))
	if errors.Is(err, pgx.ErrNoRows) {
		revision = nil
	} else if err != nil {
		return nil, nil, nil, false, fmt.Errorf("get active function revision: %w", err)
	}
	deployment, err := scanDeployment(q.QueryRow(ctx, deploymentSelectSQL()+` WHERE function_id = $1`, fn.GetID()))
	if errors.Is(err, pgx.ErrNoRows) {
		deployment = nil
	} else if err != nil {
		return nil, nil, nil, false, fmt.Errorf("get function deployment: %w", err)
	}
	return fn, revision, deployment, true, nil
}

func (s *Store) listFunctions(ctx context.Context, filter *functionv1.FunctionListFilter) ([]*functionv1.Function, string, error) {
	rows, err := s.db.Pool().Query(ctx, functionSelectSQL()+` ORDER BY created_at DESC, function_id DESC`)
	if err != nil {
		return nil, "", fmt.Errorf("query functions: %w", err)
	}
	defer rows.Close()
	out := make([]*functionv1.Function, 0)
	for rows.Next() {
		fn, err := scanFunction(rows)
		if err != nil {
			return nil, "", err
		}
		if functionkernel.MatchFunctionFilter(fn, filter) {
			out = append(out, fn)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].GetCreatedAt().AsTime().After(out[j].GetCreatedAt().AsTime())
	})
	if filter != nil {
		return pageSlice(out, filter.GetCursor(), filter.GetPageSize())
	}
	return out, "", nil
}

func (s *Store) getInvocation(ctx context.Context, invocationID string) (*functionv1.FunctionInvocation, bool, error) {
	invocation, err := scanInvocation(s.db.Pool().QueryRow(ctx, invocationSelectSQL()+` WHERE invocation_id = $1`, strings.TrimSpace(invocationID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get function invocation: %w", err)
	}
	return invocation, true, nil
}

func (s *Store) listInvocations(ctx context.Context, filter *functionv1.FunctionInvocationListFilter) ([]*functionv1.FunctionInvocation, string, error) {
	rows, err := s.db.Pool().Query(ctx, invocationSelectSQL()+` ORDER BY created_at DESC, invocation_id DESC`)
	if err != nil {
		return nil, "", fmt.Errorf("query function invocations: %w", err)
	}
	defer rows.Close()
	out := make([]*functionv1.FunctionInvocation, 0)
	for rows.Next() {
		invocation, err := scanInvocation(rows)
		if err != nil {
			return nil, "", err
		}
		if functionkernel.MatchInvocationFilter(invocation, filter) {
			out = append(out, invocation)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if filter != nil {
		return pageSlice(out, filter.GetCursor(), filter.GetPageSize())
	}
	return out, "", nil
}
