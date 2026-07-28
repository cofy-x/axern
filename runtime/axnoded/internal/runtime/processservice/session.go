package processservice

import (
	"context"
	"sort"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type OpenSessionFunc func(context.Context, *apipb.ExecSessionOpen, contract.HandlerOptions) (contract.Session, error)

type sessionBackedProcessService struct {
	openSessionFn OpenSessionFunc
}

func New(openSessionFn OpenSessionFunc) contract.ProcessService {
	return sessionBackedProcessService{openSessionFn: openSessionFn}
}

func (s sessionBackedProcessService) OpenProcess(ctx context.Context, request *apipb.ProcessOpen, options contract.HandlerOptions) (contract.Session, error) {
	return s.openSessionFn(ctx, &apipb.ExecSessionOpen{
		ID:           request.GetID(),
		Command:      request.GetCommand(),
		Tty:          request.GetTty(),
		Envs:         processKeyValues(request.GetEnv()),
		Cwd:          request.GetCwd(),
		User:         request.GetUser(),
		ManagedProxy: request.GetManagedProxy(),
	}, options)
}

func processKeyValues(values map[string]string) []*apipb.KeyValue {
	items := make([]*apipb.KeyValue, 0, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		items = append(items, &apipb.KeyValue{Key: key, Value: values[key]})
	}
	return items
}
