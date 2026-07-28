package service

import (
	"context"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
)

type runtimeSpyProcessService struct {
	handler *runtimeSpyHandler
}

func (s runtimeSpyProcessService) OpenProcess(_ context.Context, req *apipb.ProcessOpen, options contract.HandlerOptions) (contract.Session, error) {
	s.handler.lastProcessOptions = options
	s.handler.lastSessionOpen = &apipb.ExecSessionOpen{
		ID:      req.GetID(),
		Command: req.GetCommand(),
		Tty:     req.GetTty(),
		Envs:    startplan.KeyValuesFromStringMap(req.GetEnv()),
		Cwd:     req.GetCwd(),
		User:    req.GetUser(),
	}
	if s.handler.execSession != nil || s.handler.execSessionErr != nil {
		return s.handler.execSession, s.handler.execSessionErr
	}
	return &execSessionStub{}, nil
}
