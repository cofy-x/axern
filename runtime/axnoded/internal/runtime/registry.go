package runtime

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type RuntimeFactory interface {
	New(config.Config, string, config.RuntimeInstanceConfig) (contract.RuntimeHandler, error)
}

type RuntimeFactoryFunc func(config.Config, string, config.RuntimeInstanceConfig) (contract.RuntimeHandler, error)

func (f RuntimeFactoryFunc) New(cfg config.Config, runtimeName string, runtimeCfg config.RuntimeInstanceConfig) (contract.RuntimeHandler, error) {
	return f(cfg, runtimeName, runtimeCfg)
}

var runtimeFactories = map[string]RuntimeFactory{}

func RegisterRuntimeFactory(name string, factory RuntimeFactory) {
	if factory == nil {
		panic("runtime factory is nil")
	}
	runtimeFactories[name] = factory
}

func RuntimeFactoryByName(name string) (RuntimeFactory, bool) {
	factory, ok := runtimeFactories[name]
	return factory, ok
}

func RegisteredRuntimeFactories() map[string]RuntimeFactory {
	out := make(map[string]RuntimeFactory, len(runtimeFactories))
	for name, factory := range runtimeFactories {
		out[name] = factory
	}
	return out
}

func GetRuntimeHandler(cfg config.Config, runtimeName string) (contract.RuntimeHandler, error) {
	runtimeCfg, ok := cfg.RuntimeConfig.NormalizedRuntimeConfig(runtimeName)
	if !ok {
		return nil, errord.ErrNotFound
	}
	if runtimeCfg.Binary == "" {
		return nil, fmt.Errorf("runtime %s binary is not configured", runtimeName)
	}
	if _, err := os.Stat(runtimeCfg.Binary); err != nil {
		return nil, err
	}

	factory, ok := RuntimeFactoryByName(runtimeName)
	if !ok {
		return nil, errord.ErrNotImplemented
	}
	return factory.New(cfg, runtimeName, runtimeCfg)
}
